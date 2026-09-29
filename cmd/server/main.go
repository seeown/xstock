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
	"sync"
	"time"

	"github.com/bensema/gotdx/types"

	"xstock/internal/config"
	"xstock/internal/market"
	"xstock/internal/quotes"
	"xstock/internal/store"
	"xstock/internal/tdx"
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

// liveFetch caches short-lived TDX round trips (weekly/monthly/yearly index
// bars, the day's minute series) so period flipping doesn't re-hit servers.
type liveEntry struct {
	at    time.Time
	value any
}

var (
	liveMu    sync.Mutex
	liveStore = map[string]liveEntry{}
)

func liveFetch(key string, ttl time.Duration, fetch func() (any, error)) (any, error) {
	liveMu.Lock()
	if e, ok := liveStore[key]; ok && time.Since(e.at) < ttl {
		liveMu.Unlock()
		return e.value, nil
	}
	liveMu.Unlock()
	v, err := fetch()
	if err != nil {
		// TDX 服务器间歇性限流/超时（实测每次约 8 秒自愈）：
		// 稍候自动重试一次，再失败就回退到 10 分钟内的上次成功结果——
		// 分时/周期K宁可陈旧一分钟，不给前端甩空态。
		time.Sleep(400 * time.Millisecond)
		v, err = fetch()
	}
	if err != nil {
		liveMu.Lock()
		if e, ok := liveStore[key]; ok && time.Since(e.at) < 10*time.Minute {
			liveMu.Unlock()
			log.Printf("liveFetch %s 拉取失败，回退 %s 前的缓存: %v", key, time.Since(e.at).Round(time.Second), err)
			return e.value, nil
		}
		liveMu.Unlock()
		return nil, err
	}
	liveMu.Lock()
	liveStore[key] = liveEntry{at: time.Now(), value: v}
	liveMu.Unlock()
	return v, nil
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
	addr := getenv("XSTOCK_ADDR", ":8080")

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

	// 今日实时bar叠加层：快照每刷一轮就重建（合成今日K线 + 锚定校验），
	// 收盘后由 archiveToday 落库。回测不经过叠加层。快照名单含四个大盘
	// 指数——指数与个股同一链路拿到今日实时bar（大盘页KPI/日K全天实时）。
	ov := newOverlay()
	go func() {
		quoteCache.Run(context.Background(), func() []string {
			all := s.AllProfiles()
			syms := make([]string, 0, len(all)+len(tdx.IndexDefs))
			for _, p := range all {
				syms = append(syms, p.Symbol)
			}
			for _, def := range tdx.IndexDefs {
				syms = append(syms, def.Symbol)
			}
			return syms
		}, time.Minute)
	}()
	go func() {
		// 等首轮快照就绪后随每轮刷新重建叠加层。
		for range time.Tick(15 * time.Second) {
			if quoteCache.Ready() {
				ov.refresh(s, quoteCache)
			}
		}
	}()

	// 全市场情绪 + 板块统计（全量日线扫描，按 asOf 缓存）。
	mv := newMarketView(s)

	// 收盘落库（替代 dailyupdate 的核心职能）：15:05 批量写今日bar、
	// 除权票全量重拉、指数补齐，完成后 mv 缓存失效。
	arch := newArchiver(s, ov, mv, source)
	go arch.run(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	// strategy=n（通用 N 字，默认）或 zt（首板涨停→缩量回调→放量突破）。
	// 用户在库里设置的默认参数组优先；未设置或解析失败时回退内置值，
	// 反序列化只覆盖 JSON 里出现的字段——部分参数也能生效。
	mux.HandleFunc("GET /api/params/default", func(w http.ResponseWriter, r *http.Request) {
		strategy := r.URL.Query().Get("strategy")
		// 从内置值起步做部分覆盖：unmarshal 到已初始化的结构体，
		// JSON 里没出现的字段保留内置值；库默认解析失败则整组回退内置。
		if strategy == "zt" {
			zt := market.DefaultFirstBoardParams()
			if ps, err := s.DefaultParamSet(r.Context(), strategy); err != nil {
				log.Printf("读取默认参数组失败，使用内置值: %v", err)
			} else if ps != nil {
				if err := json.Unmarshal(ps.Params, &zt); err != nil {
					log.Printf("默认参数组 %s 解析失败，使用内置值: %v", ps.Name, err)
				}
			}
			writeJSON(w, 200, zt)
			return
		}
		n := market.DefaultParams()
		if ps, err := s.DefaultParamSet(r.Context(), strategy); err != nil {
			log.Printf("读取默认参数组失败，使用内置值: %v", err)
		} else if ps != nil {
			if err := json.Unmarshal(ps.Params, &n); err != nil {
				log.Printf("默认参数组 %s 解析失败，使用内置值: %v", ps.Name, err)
			}
		}
		writeJSON(w, 200, n)
	})

	// 参数组 CRUD：策略说明页的参数组管理与「设为回测默认」。
	mux.HandleFunc("GET /api/params/sets", func(w http.ResponseWriter, r *http.Request) {
		sets, err := s.ListParamSets(r.Context())
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, sets)
	})

	type paramSetBody struct {
		Name     string          `json:"name"`
		Strategy string          `json:"strategy"`
		Params   json.RawMessage `json:"params"`
	}

	mux.HandleFunc("POST /api/params/sets", func(w http.ResponseWriter, r *http.Request) {
		var body paramSetBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, 400, "invalid json: "+err.Error())
			return
		}
		if body.Name == "" || (body.Strategy != "n" && body.Strategy != "zt") || len(body.Params) == 0 {
			errorJSON(w, 400, "name, strategy (n|zt) and params are required")
			return
		}
		ps, err := s.CreateParamSet(r.Context(), body.Name, body.Strategy, body.Params)
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, ps)
	})

	mux.HandleFunc("PUT /api/params/sets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			errorJSON(w, 400, "invalid id")
			return
		}
		var body paramSetBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, 400, "invalid json: "+err.Error())
			return
		}
		if body.Name == "" || len(body.Params) == 0 {
			errorJSON(w, 400, "name and params are required")
			return
		}
		if err := s.UpdateParamSet(r.Context(), id, body.Name, body.Params); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("DELETE /api/params/sets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			errorJSON(w, 400, "invalid id")
			return
		}
		if err := s.DeleteParamSet(r.Context(), id); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/params/sets/{id}/default", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			errorJSON(w, 400, "invalid id")
			return
		}
		if err := s.SetDefaultParamSet(r.Context(), id); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	// strategy=n|zt；清除后回退内置默认值。
	mux.HandleFunc("POST /api/params/default/clear", func(w http.ResponseWriter, r *http.Request) {
		strategy := r.URL.Query().Get("strategy")
		if strategy != "n" && strategy != "zt" {
			errorJSON(w, 400, "strategy must be n or zt")
			return
		}
		if err := s.ClearDefaultParamSet(r.Context(), strategy); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/stocks", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Symbols()) })

	// Stock browser: filtered profiles over the whole market, sortable on
	// every column (price/change/amount ranking uses the quote cache).
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		watchOnly := r.URL.Query().Get("watch") == "1"
		var watchSet map[string]bool
		if watchOnly {
			set, err := s.WatchSet(r.Context())
			if err != nil {
				errorJSON(w, 500, err.Error())
				return
			}
			watchSet = set
		}
		items := s.QueryProfiles(store.ProfileFilter{
			Q:          r.URL.Query().Get("q"),
			Board:      r.URL.Query().Get("board"),
			Industry:   r.URL.Query().Get("industry"),
			Concept:    r.URL.Query().Get("concept"),
			SyncedOnly: r.URL.Query().Get("synced") == "1",
			WatchSet:   watchSet,
			WatchOnly:  watchOnly,
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

	// 盘前资讯（一期）：双源快讯 + 隔夜行情带，增量轮询。
	ns := newNewsService()
	newsRoutes(mux, s, ns)

	// GET /api/auction — 竞价异动分层读数（自选/梯队/点火/异动榜 + 晨报三问）。
	// 交易日 9:30 后的首次请求顺手归档（定格 bars + 晨报 JSONB，幂等）。
	mux.HandleFunc("GET /api/auction", func(w http.ResponseWriter, r *http.Request) {
		if !quoteCache.Ready() {
			errorJSON(w, 503, "行情快照尚未就绪")
			return
		}
		now := time.Now()
		weekday := now.Weekday() != time.Saturday && now.Weekday() != time.Sunday
		hm := now.Hour()*100 + now.Minute()
		archived := false
		if weekday && hm >= 930 {
			date := now.Format("2006-01-02")
			if _, has, _ := s.AuctionReport(r.Context(), date); !has {
				if _, err := archiveAuction(s, mv, quoteCache); err != nil {
					log.Printf("竞价自动归档失败: %v", err)
				} else {
					archived = true
				}
			} else {
				archived = true
			}
		}
		res, err := computeAuction(s, mv, quoteCache)
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		if archived {
			res.ArchivedAt = now.Format("2006-01-02 15:04")
		} else if d := now.Format("2006-01-02"); now.Weekday() != time.Saturday && now.Weekday() != time.Sunday {
			if _, has, _ := s.AuctionReport(r.Context(), d); has {
				res.ArchivedAt = d + " 已归档"
			}
		}
		writeJSON(w, 200, res)
	})

	// POST /api/auction/archive — 手动归档当日竞价定格 + 晨报。
	mux.HandleFunc("POST /api/auction/archive", func(w http.ResponseWriter, r *http.Request) {
		if !quoteCache.Ready() {
			errorJSON(w, 503, "行情快照尚未就绪")
			return
		}
		date, err := archiveAuction(s, mv, quoteCache)
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"date": date, "ok": true})
	})

	// 自选股：单用户工具，无属主。watch=1 过滤 /api/profiles 已在上面支持。
	mux.HandleFunc("GET /api/watchlist", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.WatchList(r.Context())
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/watchlist/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		if err := s.WatchAdd(r.Context(), symbol); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"symbol": symbol, "ok": true})
	})
	mux.HandleFunc("DELETE /api/watchlist/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		if err := s.WatchRemove(r.Context(), symbol); err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"symbol": symbol, "ok": true})
	})
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
			// 叠加层口径：盘中 lastDate 即今日（含实时bar），收盘落库后一致。
			if bars := ov.Bars(s, def.Symbol); len(bars) > 0 {
				v.Count = len(bars)
				v.FirstDate = bars[0].Date
				v.LastDate = bars[len(bars)-1].Date
			}
			out = append(out, v)
		}
		writeJSON(w, 200, out)
	})

	// GET /api/period-bars?symbol=000001.SH|600519.SH&period=week|month|year —
	// TDX-native coarse bars for a watchlist index or any SH/SZ stock, live
	// with a 10min cache. /api/market/index-period is the old index-only path.
	periodBars := func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.URL.Query().Get("symbol"))
		if symbol == "" {
			errorJSON(w, 400, "missing symbol")
			return
		}
		period := r.URL.Query().Get("period")
		var category uint16
		switch period {
		case "week":
			category = types.KLINE_TYPE_WEEKLY
		case "month":
			category = types.KLINE_TYPE_MONTHLY
		case "year":
			category = types.KLINE_TYPE_YEARLY
		default:
			errorJSON(w, 400, "period 仅支持 week/month/year")
			return
		}
		v, err := liveFetch("pbars:"+symbol+":"+period, 10*time.Minute, func() (any, error) {
			if def, ok := tdx.IndexOf(symbol); ok {
				return source.FetchIndexBars(def, category)
			}
			return source.FetchStockPeriodBars(symbol, category)
		})
		if err != nil {
			errorJSON(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, v)
	}
	mux.HandleFunc("GET /api/period-bars", periodBars)
	mux.HandleFunc("GET /api/market/index-period", periodBars)

	// GET /api/intraday?symbol= — the latest session's minute time-sharing
	// series (price + running average + per-minute volume) plus the previous
	// close baseline, for a watchlist index or any SH/SZ stock. 45s cache.
	// /api/market/intraday is the old index-only path.
	intraday := func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.URL.Query().Get("symbol"))
		if symbol == "" {
			errorJSON(w, 400, "missing symbol")
			return
		}
		v, err := liveFetch("min:"+symbol, 45*time.Second, func() (any, error) {
			return source.FetchMinute(symbol)
		})
		if err != nil {
			errorJSON(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, v)
	}
	mux.HandleFunc("GET /api/intraday", intraday)
	mux.HandleFunc("GET /api/market/intraday", intraday)

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
		// 叠加今日实时bar（锚定校验通过的票）；?closed=1 只看收盘序列。
		var bars []market.Candle
		if r.URL.Query().Get("closed") == "1" {
			bars = s.Bars(symbol)
		} else {
			bars = ov.Bars(s, symbol)
		}
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

	// GET /api/market/guide — 情绪指南：量能+涨跌停的温度判定（实时+30日序列）。
	mux.HandleFunc("GET /api/market/guide", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, mv.guidePayload(quoteCache))
	})

	mux.HandleFunc("GET /api/screen", func(w http.ResponseWriter, r *http.Request) {
		days := queryInt(r, "days", 10)
		asOf, counts, items, _ := scanScreen(s, ov, days)
		writeJSON(w, 200, map[string]any{
			"asOf": asOf, "windowDays": days, "counts": counts, "items": items,
		})
	})

	// POST /api/screen/archive — 立即归档：服务端现扫全市场（与筛查同口径，
	// 含今日实时bar），以数据基准日为归档日全量替换入库（重复归档以最后为准）。
	mux.HandleFunc("POST /api/screen/archive", func(w http.ResponseWriter, r *http.Request) {
		res, err := archiveScreenNow(r, s, ov)
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, res)
	})

	// GET /api/screen/archives — 归档日期列表（近 → 远）。
	mux.HandleFunc("GET /api/screen/archives", func(w http.ResponseWriter, r *http.Request) {
		dates, err := s.ListScreenArchiveDates(r.Context())
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, dates)
	})

	// GET /api/screen/archive?date= — 某日归档形态 + 买点日/价与
	// T+1/T+5/T+10/期间最高（从日线动态计算，不落库）。
	mux.HandleFunc("GET /api/screen/archive", func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		if date == "" {
			errorJSON(w, 400, "missing date")
			return
		}
		items, counts, err := archivedScreenOfDay(r, s, ov, date)
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"date": date, "counts": counts, "items": items})
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

	log.Printf("xstock API listening on %s (postgres: %s, demo symbol: DEMO)", listenURL(addr), pgSummary())
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
