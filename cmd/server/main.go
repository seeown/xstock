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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/params/default", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, market.DefaultParams()) })
	mux.HandleFunc("GET /api/stocks", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Symbols()) })

	// Stock browser: filtered/paginated profiles over the whole market.
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		page := queryInt(r, "page", 1)
		pageSize := queryInt(r, "pageSize", 50)
		result := s.QueryProfiles(store.ProfileFilter{
			Q:          r.URL.Query().Get("q"),
			Board:      r.URL.Query().Get("board"),
			Industry:   r.URL.Query().Get("industry"),
			Concept:    r.URL.Query().Get("concept"),
			SyncedOnly: r.URL.Query().Get("synced") == "1",
			Page:       page,
			PageSize:   pageSize,
		})
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/concepts", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.ConceptCounts()) })
	mux.HandleFunc("GET /api/industries", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Industries()) })

	// GET /api/quotes?symbols=600519.SH,000001.SZ — latest prices for one page.
	mux.HandleFunc("GET /api/quotes", func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("symbols")
		var symbols []string
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				symbols = append(symbols, strings.ToUpper(part))
			}
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

	mux.HandleFunc("GET /api/stocks/{symbol}/signals", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		bars := s.Bars(symbol)
		if bars == nil {
			errorJSON(w, 404, "stock not found")
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
			Params      market.NParams `json:"params"`
			InitialCash float64        `json:"initialCash"`
		}{Params: market.DefaultParams(), InitialCash: 100000}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errorJSON(w, 400, "invalid JSON")
			return
		}
		if err := market.ValidParams(req.Params); err != nil {
			errorJSON(w, 400, err.Error())
			return
		}
		bars := s.Bars(symbol)
		if bars == nil {
			errorJSON(w, 404, "stock not found")
			return
		}
		writeJSON(w, 200, market.Backtest(symbol, bars, req.Params, req.InitialCash))
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

func listenURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}
