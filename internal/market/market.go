// Package market contains the N-pattern strategy, backtest engine and the
// shared daily-bar types used by data sources and the HTTP layer.
package market

import (
	"errors"
	"math"
	"time"
)

var errInvalidParams = errors.New("invalid strategy parameters")

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
	Symbol        string  `json:"symbol"`
	Date          string  `json:"date"`
	Reason        string  `json:"reason"`
	BreakoutPrice float64 `json:"breakoutPrice"`
	PriorHigh     float64 `json:"priorHigh"`
	PullbackLow   float64 `json:"pullbackLow"`
	RisePct       float64 `json:"risePct"`
	PullbackPct   float64 `json:"pullbackPct"`
	VolumeRatio   float64 `json:"volumeRatio"`
}

type Trade struct {
	Symbol     string  `json:"symbol"`
	BuyDate    string  `json:"buyDate"`
	SellDate   string  `json:"sellDate"`
	ExitReason string  `json:"exitReason"`
	BuyPrice   float64 `json:"buyPrice"`
	SellPrice  float64 `json:"sellPrice"`
	ReturnPct  float64 `json:"returnPct"`
}

type Metrics struct {
	InitialCash    float64 `json:"initialCash"`
	FinalCash      float64 `json:"finalCash"`
	TotalReturnPct float64 `json:"totalReturnPct"`
	MaxDrawdownPct float64 `json:"maxDrawdownPct"`
	WinRatePct     float64 `json:"winRatePct"`
	TradeCount     int     `json:"tradeCount"`
}

type BacktestResult struct {
	Signals []Signal `json:"signals"`
	Trades  []Trade  `json:"trades"`
	Metrics Metrics  `json:"metrics"`
}

func ValidParams(p NParams) error {
	if p.RiseDays < 2 || p.PullbackMinDays < 1 || p.PullbackMaxDays < p.PullbackMinDays ||
		p.RiseMinPct <= 0 || p.PullbackMaxPct <= 0 || p.VolumeRatioMin <= 0 ||
		p.StopLossPct <= 0 || p.TakeProfitPct <= 0 || p.MaxHoldDays < 1 {
		return errInvalidParams
	}
	return nil
}

// FindNSignals identifies: rising leg -> lower-volume pullback -> volume-backed breakout.
// A signal is emitted at the breakout close; the backtest enters at the next open.
func FindNSignals(symbol string, bars []Candle, p NParams) []Signal {
	var out []Signal
	if ValidParams(p) != nil {
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

// DemoBars builds the built-in DEMO series so the dashboard works offline.
func DemoBars() []Candle {
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
