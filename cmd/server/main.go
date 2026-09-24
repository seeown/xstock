package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"nstock/internal/config"
	"nstock/internal/market"
	"nstock/internal/quotes"
	"nstock/internal/store"
	"nstock/internal/tdx"
)

//go:embed all:web/dist
var distFS embed.FS

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func normalizeBars(bars []market.Candle) []market.Candle {
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	return bars
}

func main() {
	if err := config.LoadEnv(); err != nil {
		log.Fatalf("load %s: %v — create it at the project root with the PostgreSQL settings", config.EnvFile, err)
	}
	dsn, err := config.PGDSN()
	if err != nil {
		log.Fatal(err)
	}
	addr := getenv("NSTOCK_ADDR", ":8080")

	// Loading the full market's bars (16M+ rows) into the cache can take a
	// few minutes after a bulk import; keep a generous hard cap.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	s, err := store.Open(ctx, dsn)
	cancel()
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()
	s.Seed("DEMO", market.DemoBars())

	source := tdx.New()
	defer func() {
		if err := source.Close(); err != nil {
			log.Printf("close tdx client: %v", err)
		}
	}()

	// Background whole-market quote snapshot for ranking columns.
	quoteCache := quotes.New(3)
	defer quoteCache.Close()
	go quoteCache.Run(context.Background(), func() []string {
		all := s.AllProfiles()
		syms := make([]string, 0, len(all))
		for _, p := range all {
			syms = append(syms, p.Symbol)
		}
		return syms
	}, time.Minute)

	// 全市场情绪 + 板块统计（全量日线扫描，按 asOf 缓存）。
	mv := newMarketView(s)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	// strategy=n（通用 N 字，默认）或 zt（首板涨停→缩量回调→放量突破）。
	mux.HandleFunc("GET /api/params/default", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("strategy") == "zt" {
			writeJSON(w, 200, market.DefaultFirstBoardParams())
			return
		}
		writeJSON(w, 200, market.DefaultParams())
	})
	mux.HandleFunc("GET /api/stocks", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Symbols()) })

	// Stock browser: filtered profiles over the whole market, sortable on
	// every column (price/change/amount ranking uses the quote cache).
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		items := s.QueryProfiles(store.ProfileFilter{
			Q:          r.URL.Query().Get("q"),
			Board:      r.URL.Query().Get("board"),
			Industry:   r.URL.Query().Get("industry"),
			Concept:    r.URL.Query().Get("concept"),
			SyncedOnly: r.URL.Query().Get("synced") == "1",
		})
		sortProfiles(items, r.URL.Query().Get("sort"), r.URL.Query().Get("order") == "desc", quoteCache.Snapshot())
		page := queryInt(r, "page", 1)
		pageSize := queryInt(r, "pageSize", 50)
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 200 {
			pageSize = 50
		}
		total := len(items)
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		writeJSON(w, 200, map[string]any{
			"items": items[start:end], "total": total, "page": page, "pageSize": pageSize,
		})
	})
	mux.HandleFunc("GET /api/concepts", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.ConceptCounts()) })
	mux.HandleFunc("GET /api/industries", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Industries()) })

	// GET /api/quotes?symbols=600519.SH,000001.SZ — latest prices for one
	// page. Served from the background quote cache when it is warm.
	mux.HandleFunc("GET /api/quotes", func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("symbols")
		var symbols []string
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				symbols = append(symbols, strings.ToUpper(part))
			}
		}
		if quoteCache.Ready() {
			snap := quoteCache.Snapshot()
			out := make([]tdx.Quote, 0, len(symbols))
			for _, sym := range symbols {
				if q, ok := snap[sym]; ok {
					out = append(out, q)
				}
			}
			writeJSON(w, 200, out)
			return
		}
		quotes, err := source.FetchQuotes(symbols)
		if err != nil {
			errorJSON(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, quotes)
	})
	mux.HandleFunc("GET /api/market/indices", func(w http.ResponseWriter, r *http.Request) {
		type indexView struct {
			Symbol    string `json:"symbol"`
			Name      string `json:"name"`
			Count     int    `json:"count,omitempty"`
			FirstDate string `json:"firstDate,omitempty"`
			LastDate  string `json:"lastDate,omitempty"`
		}
		out := make([]indexView, 0, len(tdx.IndexDefs))
		for _, def := range tdx.IndexDefs {
			v := indexView{Symbol: def.Symbol, Name: def.Name}
			if bars := s.Bars(def.Symbol); len(bars) > 0 {
				v.Count = len(bars)
				v.FirstDate = bars[0].Date
				v.LastDate = bars[len(bars)-1].Date
			}
			out = append(out, v)
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("POST /api/bars/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		var bars []market.Candle
		if err := json.NewDecoder(r.Body).Decode(&bars); err != nil || len(bars) == 0 {
			errorJSON(w, 400, "body must be a non-empty candle array")
			return
		}
		if err := s.Replace(r.Context(), symbol, normalizeBars(bars)); err != nil {
			log.Printf("sync %s: replace failed: %v", symbol, err)
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"symbol": symbol, "count": len(bars)})
	})

	// POST /api/sync/{symbol} downloads the full QFQ daily history for a real
	// SH/SZ symbol from TDX quote servers and persists it locally.
	mux.HandleFunc("POST /api/sync/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		if def, ok := tdx.IndexOf(symbol); ok {
			bars, err := source.FetchDailyIndex(def)
			if err != nil {
				errorJSON(w, 502, err.Error())
				return
			}
			if err := s.Replace(r.Context(), symbol, bars); err != nil {
				errorJSON(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, map[string]any{
				"symbol":    symbol,
				"count":     len(bars),
				"firstDate": bars[0].Date,
				"lastDate":  bars[len(bars)-1].Date,
			})
			return
		}
		if symbol == "DEMO" {
			errorJSON(w, 400, "DEMO 是内置演示数据；请输入真实代码，例如 600519.SH")
			return
		}
		bars, err := source.FetchDailyQFQ(symbol)
		if err != nil {
			errorJSON(w, 502, err.Error())
			return
		}
		if err := s.Replace(r.Context(), symbol, bars); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{
			"symbol":    symbol,
			"count":     len(bars),
			"firstDate": bars[0].Date,
			"lastDate":  bars[len(bars)-1].Date,
		})
	})

	// POST /api/profile/sync/{symbol} refreshes one stock's metadata: F10 text
	// (name / industry / business), IPO date and TDX concept tags.
	mux.HandleFunc("POST /api/profile/sync/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		if _, isIndex := tdx.IndexOf(symbol); isIndex {
			errorJSON(w, 400, "指数没有个股档案；请输入股票代码，例如 600519.SH")
			return
		}
		if symbol == "DEMO" {
			errorJSON(w, 400, "DEMO 是内置演示数据；请输入真实代码，例如 600519.SH")
			return
		}
		fp, err := source.FetchProfile(symbol)
		if err != nil {
			errorJSON(w, 502, err.Error())
			return
		}
		p := store.Profile{
			Symbol: symbol, Name: fp.Name, Industry: fp.Industry, Market: fp.Market,
			Board: fp.Board, ListDate: fp.ListDate, Business: fp.Business,
		}
		if fp.Concepts != nil {
			p.Concepts = fp.Concepts
		}
		if err := s.SaveProfile(r.Context(), p); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		if saved, ok := s.Profile(symbol); ok {
			p = saved
		}
		writeJSON(w, 200, p)
	})

	mux.HandleFunc("GET /api/stocks/{symbol}/profile", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		p, ok := s.Profile(symbol)
		if !ok {
			errorJSON(w, 404, "profile not synced")
			return
		}
		writeJSON(w, 200, p)
	})

	// Hot cache refresh after the scheduled daily updater writes to PG.
	mux.HandleFunc("POST /api/admin/reload", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		if err := s.Reload(ctx); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		mv.invalidate()
		writeJSON(w, 200, map[string]any{"status": "reloaded"})
	})
	mux.HandleFunc("GET /api/sync-state", func(w http.ResponseWriter, r *http.Request) {
		st, err := s.GetSyncState(r.Context())
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, st)
	})

	mux.HandleFunc("GET /api/stocks/{symbol}/signals", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		bars := s.Bars(symbol)
		if bars == nil {
			errorJSON(w, 404, "stock not found")
			return
		}
		if r.URL.Query().Get("strategy") == "zt" {
			writeJSON(w, 200, market.FindFirstBoardSignals(symbol, bars, market.DefaultFirstBoardParams()))
			return
		}
		writeJSON(w, 200, market.FindNSignals(symbol, bars, market.DefaultParams()))
	})
	mux.HandleFunc("GET /api/stocks/{symbol}/bars", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		bars := s.Bars(symbol)
		if bars == nil {
			errorJSON(w, 404, "stock not found")
			return
		}
		writeJSON(w, 200, bars)
	})
	mux.HandleFunc("POST /api/backtests/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		req := struct {
			Strategy    string          `json:"strategy"` // "n"（默认）或 "zt"
			Params      json.RawMessage `json:"params"`   // 留空则用该策略默认参数
			InitialCash float64         `json:"initialCash"`
			Days        int             `json:"days"` // >0: 只统计最近 N 个交易日；0: 全部历史
		}{InitialCash: 100000}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errorJSON(w, 400, "invalid JSON")
			return
		}
		bars := s.Bars(symbol)
		if bars == nil {
			errorJSON(w, 404, "stock not found")
			return
		}
		var res market.BacktestResult
		switch req.Strategy {
		case "", "n":
			p := market.DefaultParams()
			if len(req.Params) > 0 {
				if err := json.Unmarshal(req.Params, &p); err != nil {
					errorJSON(w, 400, "invalid n params")
					return
				}
			}
			if err := market.ValidParams(p); err != nil {
				errorJSON(w, 400, err.Error())
				return
			}
			res = market.Backtest(symbol, bars, p, req.InitialCash)
		case "zt":
			p := market.DefaultFirstBoardParams()
			if len(req.Params) > 0 {
				if err := json.Unmarshal(req.Params, &p); err != nil {
					errorJSON(w, 400, "invalid zt params")
					return
				}
			}
			if err := market.ValidFirstBoardParams(p); err != nil {
				errorJSON(w, 400, err.Error())
				return
			}
			res = market.FirstBoardBacktest(symbol, bars, p, req.InitialCash)
		default:
			errorJSON(w, 400, "unknown strategy: "+req.Strategy)
			return
		}
		res = market.WindowResult(res, bars, req.Days)
		writeJSON(w, 200, res)
	})

	// GET /api/screen?days=10 — 全市场 N 字战法筛查：跟踪每只票
	// 首板→回调(B1)→突破(B2)→回踩(B3) 的存活形态，按各阶段关键日期
	// 过滤出近 days 个交易日内出现的 setup。ST 与上市过新的票直接排除。
	// GET /api/market/sentiment — 市场情绪：实时口径（快照）+ 近30日趋势。
	mux.HandleFunc("GET /api/market/sentiment", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, mv.sentimentPayload(quoteCache))
	})
	// GET /api/market/sectors — 板块聚合：行业为主、概念为辅（概念成员
	// 400 上限截断，家数偏保守），实时均涨/封板覆盖 + 主线识别。
	mux.HandleFunc("GET /api/market/sectors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, mv.sectorsPayload(quoteCache))
	})

	mux.HandleFunc("GET /api/screen", func(w http.ResponseWriter, r *http.Request) {
		days := queryInt(r, "days", 10)
		p := market.DefaultScreenParams()
		type screenItem struct {
			market.ScreenSetup
			Name     string `json:"name"`
			Industry string `json:"industry,omitempty"`
			sortKey  string
		}
		items := make([]screenItem, 0, 64)
		counts := map[string]int{"b1": 0, "b2": 0, "b3": 0}
		asOf := ""
		for _, info := range s.Symbols() {
			sym := info.Symbol
			bars := s.Bars(sym)
			if len(bars) < 60 {
				continue // 上市过新：MA20/量比前置不足
			}
			prof, hasProf := s.Profile(sym)
			if hasProf && strings.Contains(strings.ToUpper(prof.Name), "ST") {
				continue // ST 5% 限额无法按日线识别，整体排除
			}
			start := market.WindowStart(bars, days)
			for _, setup := range market.FindNSetups(sym, bars, p) {
				// B1 买点 = 回调阶段第一次止跌的K线（回调≥2天后的首根阳线）。
				// 尚未止跌（刚首板/回调中无阳线）的票不进 B1 名单；
				// 时间窗口与排序也按止跌触发日而非首板日。
				key := setup.KeyDate
				if setup.Stage == "b1" {
					if !setup.B1Triggered {
						continue
					}
					key = setup.B1TriggerDate
				}
				if start != "" && key < start {
					continue
				}
				counts[setup.Stage]++
				it := screenItem{ScreenSetup: setup, sortKey: key}
				if hasProf {
					it.Name, it.Industry = prof.Name, prof.Industry
				}
				items = append(items, it)
				if setup.AsOf > asOf {
					asOf = setup.AsOf
				}
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].sortKey != items[j].sortKey {
				return items[i].sortKey > items[j].sortKey
			}
			return items[i].Symbol < items[j].Symbol
		})
		writeJSON(w, 200, map[string]any{
			"asOf": asOf, "windowDays": days, "counts": counts, "items": items,
		})
	})

	static, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		log.Fatalf("static assets: %v", err)
	}
	fileServer := http.FileServer(http.FS(static))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(static, path); err != nil {
			// SPA history fallback: unknown non-asset paths serve index.html.
			r.URL.Path = "/"
			path = "index.html"
		}
		// Vite content-hashes asset filenames, so they can be cached
		// forever; index.html must always revalidate or browsers keep
		// referencing retired asset hashes after a rebuild (blank page).
		if path == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})

	log.Printf("NStock API listening on %s (postgres: %s, demo symbol: DEMO)", listenURL(addr), pgSummary())
	log.Fatal(http.ListenAndServe(addr, mux))
}

func pgSummary() string {
	return os.Getenv("PG_HOST") + ":" + os.Getenv("PG_PORT") + "/" + os.Getenv("DB_NAME")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func queryInt(r *http.Request, key string, fallback int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return fallback
}

// sortProfiles orders the browser list in place. Quote-backed keys (price,
// changePct, amount) fall back to symbol order until the quote cache warms up.
func sortProfiles(items []store.ProfileListItem, key string, desc bool, snap map[string]tdx.Quote) {
	quoteVal := func(sym string, pick func(tdx.Quote) float64) float64 {
		if q, ok := snap[sym]; ok {
			return pick(q)
		}
		return -1 // rows without quotes sink in descending rankings
	}
	less := func(a, b store.ProfileListItem) bool { return a.Symbol < b.Symbol }
	switch key {
	case "name":
		less = func(a, b store.ProfileListItem) bool { return a.Name < b.Name }
	case "board":
		less = func(a, b store.ProfileListItem) bool { return a.Board < b.Board }
	case "industry":
		less = func(a, b store.ProfileListItem) bool { return a.Industry < b.Industry }
	case "listDate":
		less = func(a, b store.ProfileListItem) bool { return a.ListDate < b.ListDate }
	case "concepts":
		less = func(a, b store.ProfileListItem) bool { return len(a.Concepts) < len(b.Concepts) }
	case "bars":
		less = func(a, b store.ProfileListItem) bool { return a.BarCount < b.BarCount }
	case "price":
		less = func(a, b store.ProfileListItem) bool {
			return quoteVal(a.Symbol, func(q tdx.Quote) float64 { return q.Price }) < quoteVal(b.Symbol, func(q tdx.Quote) float64 { return q.Price })
		}
	case "changePct":
		less = func(a, b store.ProfileListItem) bool {
			return quoteVal(a.Symbol, func(q tdx.Quote) float64 { return q.ChangePct }) < quoteVal(b.Symbol, func(q tdx.Quote) float64 { return q.ChangePct })
		}
	case "amount":
		less = func(a, b store.ProfileListItem) bool {
			return quoteVal(a.Symbol, func(q tdx.Quote) float64 { return q.Amount }) < quoteVal(b.Symbol, func(q tdx.Quote) float64 { return q.Amount })
		}
	default:
		key = "symbol"
	}
	sort.SliceStable(items, func(i, j int) bool {
		if desc {
			return less(items[j], items[i])
		}
		return less(items[i], items[j])
	})
}

func listenURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}
