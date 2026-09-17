package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed web/index.html
var dashboardHTML []byte

// Candle uses daily, adjusted OHLCV prices. Date must use YYYY-MM-DD.
type Candle struct {
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type NParams struct {
	RiseDays          int     `json:"riseDays"`
	RiseMinPct        float64 `json:"riseMinPct"`
	PullbackMinDays   int     `json:"pullbackMinDays"`
	PullbackMaxDays   int     `json:"pullbackMaxDays"`
	PullbackMaxPct    float64 `json:"pullbackMaxPct"`
	VolumeRatioMin    float64 `json:"volumeRatioMin"`
	BreakoutBufferPct float64 `json:"breakoutBufferPct"`
	StopLossPct       float64 `json:"stopLossPct"`
	TakeProfitPct     float64 `json:"takeProfitPct"`
	MaxHoldDays       int     `json:"maxHoldDays"`
}

func DefaultParams() NParams {
	return NParams{RiseDays: 10, RiseMinPct: 15, PullbackMinDays: 3, PullbackMaxDays: 10,
		PullbackMaxPct: 8, VolumeRatioMin: 1.5, BreakoutBufferPct: 0, StopLossPct: 7,
		TakeProfitPct: 15, MaxHoldDays: 20}
}

type Signal struct {
	Symbol, Date, Reason                                                     string
	BreakoutPrice, PriorHigh, PullbackLow, RisePct, PullbackPct, VolumeRatio float64
}

type Trade struct {
	Symbol, BuyDate, SellDate, ExitReason string
	BuyPrice, SellPrice, ReturnPct        float64
}

type Metrics struct {
	InitialCash, FinalCash, TotalReturnPct, MaxDrawdownPct, WinRatePct float64
	TradeCount                                                         int
}

type BacktestResult struct {
	Signals []Signal `json:"signals"`
	Trades  []Trade  `json:"trades"`
	Metrics Metrics  `json:"metrics"`
}

type store struct {
	mu   sync.RWMutex
	bars map[string][]Candle
}

func validParams(p NParams) error {
	if p.RiseDays < 2 || p.PullbackMinDays < 1 || p.PullbackMaxDays < p.PullbackMinDays ||
		p.RiseMinPct <= 0 || p.PullbackMaxPct <= 0 || p.VolumeRatioMin <= 0 ||
		p.StopLossPct <= 0 || p.TakeProfitPct <= 0 || p.MaxHoldDays < 1 {
		return fmt.Errorf("invalid strategy parameters")
	}
	return nil
}

// FindNSignals identifies: rising leg -> lower-volume pullback -> volume-backed breakout.
// A signal is emitted at the breakout close; the backtest enters at the next open.
func FindNSignals(symbol string, bars []Candle, p NParams) []Signal {
	var out []Signal
	if validParams(p) != nil {
		return out
	}
	for breakout := p.RiseDays + p.PullbackMinDays; breakout < len(bars); breakout++ {
		for pullDays := p.PullbackMinDays; pullDays <= p.PullbackMaxDays; pullDays++ {
			riseStart := breakout - pullDays - p.RiseDays
			pullStart := breakout - pullDays
			if riseStart < 0 {
				continue
			}
			start, priorHigh := bars[riseStart].Close, bars[pullStart].High
			for i := riseStart; i < pullStart; i++ {
				priorHigh = math.Max(priorHigh, bars[i].High)
			}
			risePct := (priorHigh/start - 1) * 100
			if risePct < p.RiseMinPct {
				continue
			}
			pullLow, riseVol, pullVol := bars[pullStart].Low, 0.0, 0.0
			for i := riseStart; i < pullStart; i++ {
				riseVol += bars[i].Volume
			}
			for i := pullStart; i < breakout; i++ {
				pullLow = math.Min(pullLow, bars[i].Low)
				pullVol += bars[i].Volume
			}
			pullPct := (priorHigh - pullLow) / priorHigh * 100
			if pullPct > p.PullbackMaxPct || pullLow <= start {
				continue
			}
			avgRiseVol, avgPullVol := riseVol/float64(p.RiseDays), pullVol/float64(pullDays)
			if avgPullVol >= avgRiseVol {
				continue
			} // pullback must contract
			volumeRatio := bars[breakout].Volume / avgPullVol
			requiredBreakout := priorHigh * (1 + p.BreakoutBufferPct/100)
			if bars[breakout].Close < requiredBreakout || volumeRatio < p.VolumeRatioMin {
				continue
			}
			out = append(out, Signal{Symbol: symbol, Date: bars[breakout].Date, BreakoutPrice: bars[breakout].Close,
				PriorHigh: priorHigh, PullbackLow: pullLow, RisePct: risePct, PullbackPct: pullPct,
				VolumeRatio: volumeRatio, Reason: "上涨-缩量回调-放量突破"})
			break
		}
	}
	return out
}

func Backtest(symbol string, bars []Candle, p NParams, initialCash float64) BacktestResult {
	if initialCash <= 0 {
		initialCash = 100000
	}
	signals := FindNSignals(symbol, bars, p)
	byDate := map[string]bool{}
	for _, s := range signals {
		byDate[s.Date] = true
	}
	result := BacktestResult{Signals: signals}
	cash, peak, maxDD := initialCash, initialCash, 0.0
	for i := 1; i < len(bars); {
		// Yesterday's close signal is the only information used for today's entry.
		if !byDate[bars[i-1].Date] {
			i++
			continue
		}
		buy := bars[i].Open
		if buy <= 0 {
			i++
			continue
		}
		sell, sellDate, reason := 0.0, "", "max_hold"
		last := min(i+p.MaxHoldDays-1, len(bars)-1)
		exitIndex := last
		for j := i; j <= last; j++ {
			if bars[j].Low <= buy*(1-p.StopLossPct/100) {
				sell, sellDate, reason = buy*(1-p.StopLossPct/100), bars[j].Date, "stop_loss"
				exitIndex = j
				break
			}
			if bars[j].High >= buy*(1+p.TakeProfitPct/100) {
				sell, sellDate, reason = buy*(1+p.TakeProfitPct/100), bars[j].Date, "take_profit"
				exitIndex = j
				break
			}
		}
		if sell == 0 {
			sell, sellDate = bars[last].Close, bars[last].Date
		}
		ret := (sell/buy - 1) * 100
		cash *= sell / buy
		peak = math.Max(peak, cash)
		maxDD = math.Max(maxDD, (peak-cash)/peak*100)
		result.Trades = append(result.Trades, Trade{Symbol: symbol, BuyDate: bars[i].Date, SellDate: sellDate, BuyPrice: buy, SellPrice: sell, ReturnPct: ret, ExitReason: reason})
		// One-position MVP: ignore later signals until this position is closed.
		i = exitIndex + 1
	}
	wins := 0
	for _, t := range result.Trades {
		if t.ReturnPct > 0 {
			wins++
		}
	}
	result.Metrics = Metrics{InitialCash: initialCash, FinalCash: cash, TotalReturnPct: (cash/initialCash - 1) * 100,
		MaxDrawdownPct: maxDD, TradeCount: len(result.Trades)}
	if len(result.Trades) > 0 {
		result.Metrics.WinRatePct = float64(wins) / float64(len(result.Trades)) * 100
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func errorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func main() {
	s := &store{bars: map[string][]Candle{"DEMO": demoBars()}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(dashboardHTML)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/params/default", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, DefaultParams()) })
	mux.HandleFunc("POST /api/bars/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		var bars []Candle
		if err := json.NewDecoder(r.Body).Decode(&bars); err != nil || len(bars) == 0 {
			errorJSON(w, 400, "body must be a non-empty candle array")
			return
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
		s.mu.Lock()
		s.bars[symbol] = bars
		s.mu.Unlock()
		writeJSON(w, 201, map[string]any{"symbol": symbol, "count": len(bars)})
	})
	mux.HandleFunc("GET /api/stocks/{symbol}/signals", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		p := DefaultParams()
		s.mu.RLock()
		bars := s.bars[symbol]
		s.mu.RUnlock()
		if bars == nil {
			errorJSON(w, 404, "stock not found")
			return
		}
		writeJSON(w, 200, FindNSignals(symbol, bars, p))
	})
	mux.HandleFunc("GET /api/stocks/{symbol}/bars", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		s.mu.RLock()
		bars := s.bars[symbol]
		s.mu.RUnlock()
		if bars == nil {
			errorJSON(w, 404, "stock not found")
			return
		}
		writeJSON(w, 200, bars)
	})
	mux.HandleFunc("POST /api/backtests/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.ToUpper(r.PathValue("symbol"))
		req := struct {
			Params      NParams `json:"params"`
			InitialCash float64 `json:"initialCash"`
		}{Params: DefaultParams(), InitialCash: 100000}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errorJSON(w, 400, "invalid JSON")
			return
		}
		if err := validParams(req.Params); err != nil {
			errorJSON(w, 400, err.Error())
			return
		}
		s.mu.RLock()
		bars := s.bars[symbol]
		s.mu.RUnlock()
		if bars == nil {
			errorJSON(w, 404, "stock not found")
			return
		}
		writeJSON(w, 200, Backtest(symbol, bars, req.Params, req.InitialCash))
	})
	log.Println("NStock API listening on http://localhost:8080 (demo symbol: DEMO)")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func demoBars() []Candle {
	start, _ := time.Parse("2006-01-02", "2025-01-02")
	closes := []float64{10, 10.1, 10.3, 10.6, 10.9, 11.2, 11.5, 11.8, 12, 12.2, 12.1, 11.9, 11.7, 11.8, 11.9, 12.5, 12.8, 13.0, 12.7, 12.5, 12.3, 12.7, 13.2, 13.5, 13.8, 13.4, 13.2, 13.0, 13.1, 13.3, 14.0, 14.3}
	bars := make([]Candle, 0, len(closes))
	for i, close := range closes {
		vol := 100.0
		if i >= 10 && i <= 14 {
			vol = 60
		}
		if i == 15 || i == 22 || i == 30 {
			vol = 180
		}
		bars = append(bars, Candle{Date: start.AddDate(0, 0, i).Format("2006-01-02"), Open: close * .995, High: close * 1.01, Low: close * .99, Close: close, Volume: vol})
	}
	return bars
}
