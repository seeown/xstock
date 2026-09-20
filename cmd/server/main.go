package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"nstock/internal/market"
	"nstock/internal/store"
	"nstock/internal/tdx"
)

//go:embed web/index.html
var dashboardHTML []byte

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
	dbPath := getenv("NSTOCK_DB", "nstock.db")
	addr := getenv("NSTOCK_ADDR", ":8080")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	s, err := store.Open(ctx, "file:"+dbPath)
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
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(dashboardHTML)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/params/default", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, market.DefaultParams()) })

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

	log.Printf("NStock API listening on %s (db: %s, demo symbol: DEMO)", listenURL(addr), dbPath)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
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
