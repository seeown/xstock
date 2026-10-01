package main

// 模拟仓：A股规则口径的纸面交易——市价单即时撮合（盘中用实时快照价，
// 快照缺失回落日线最后收盘）、100 股整数手、T+1（可卖量从流水推导）、
// 佣金(万2.5 最低5元)+过户费(万0.1)双边、印花税(千0.5)卖出。
// 费率做成包级常量，口径调整改这里。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"xstock/internal/quotes"
	"xstock/internal/store"
)

const (
	paperAccountName  = "默认账户"
	paperInitialCash  = 500000
	paperCommissionRT = 0.00025 // 佣金万 2.5
	paperMinFee       = 5.0     // 佣金最低 5 元
	paperTransferRT   = 0.00001 // 过户费万 0.1（双边）
	paperStampTaxRT   = 0.0005  // 印花税千 0.5（卖出）
)

func round2f(v float64) float64 {
	if v < 0 {
		return float64(int(v*100-0.5)) / 100
	}
	return float64(int(v*100+0.5)) / 100
}

// paperFees 佣金+过户费（双边）；印花税仅卖出。
func paperFees(side string, amount float64) (fee, tax float64) {
	fee = amount * paperCommissionRT
	if fee < paperMinFee {
		fee = paperMinFee
	}
	fee += amount * paperTransferRT
	if side == "sell" {
		tax = amount * paperStampTaxRT
	}
	return round2f(fee), round2f(tax)
}

// paperQuote 快照里的实时行情（无则零值）。
func paperQuote(qc *quotes.Cache, symbol string) (price, chgPct float64) {
	snap := qc.Snapshot()
	q, ok := snap[symbol]
	if !ok || q.Price <= 0 {
		return 0, 0
	}
	return q.Price, q.ChangePct
}

// paperPrice 服务端定价：实时快照优先，回落日线最后收盘。
func paperPrice(s *store.Store, qc *quotes.Cache, ov *overlay, symbol string) float64 {
	if p, _ := paperQuote(qc, symbol); p > 0 {
		return p
	}
	if bars := ov.Bars(s, symbol); len(bars) > 0 {
		return bars[len(bars)-1].Close
	}
	return 0
}

type paperOrderReq struct {
	Symbol string          `json:"symbol"`
	Name   string          `json:"name"`
	Side   string          `json:"side"` // buy | sell
	Qty    int             `json:"qty"`
	Note   string          `json:"note"`
	Signal json.RawMessage `json:"signal"` // 下单时的雷达信号上下文
}

// paperOverview 账户+持仓(实时估值)+净值曲线+流水。
func paperOverview(r *http.Request, s *store.Store, qc *quotes.Cache, ov *overlay) (map[string]any, error) {
	acct, err := s.EnsurePaperAccount(r.Context(), paperAccountName, paperInitialCash)
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	raws, err := s.PaperRawPositions(r.Context(), acct.ID, today)
	if err != nil {
		return nil, err
	}
	positions := make([]map[string]any, 0, len(raws))
	marketValue := 0.0
	dayPnl := 0.0
	for _, p := range raws {
		last, chgPct := paperQuote(qc, p.Symbol)
		if last <= 0 {
			if bars := ov.Bars(s, p.Symbol); len(bars) > 0 {
				last = bars[len(bars)-1].Close
			}
		}
		mv := round2f(last * float64(p.Qty))
		marketValue += mv
		cost := round2f(p.CostPrice * float64(p.Qty))
		pnl := round2f(mv - cost)
		pnlPct := 0.0
		if cost > 0 {
			pnlPct = round2f(mv/cost*100 - 100)
		}
		// 当日盈亏：隔夜仓由当日涨幅反推今晨成本；今日买入部分从成交价
		// （含费）起算——当天建仓的票不能把隔夜涨幅算成"今日盈亏"。
		dp := 0.0
		overnight := p.Qty - p.TodayQty
		if overnight > 0 && chgPct != 0 {
			overnightMV := last * float64(overnight)
			dp += overnightMV - overnightMV/(1+chgPct/100)
		}
		if p.TodayQty > 0 {
			dp += last*float64(p.TodayQty) - p.TodayCost
		}
		dp = round2f(dp)
		dayPnl += dp
		positions = append(positions, map[string]any{
			"symbol": p.Symbol, "name": p.Name, "qty": p.Qty, "availQty": p.AvailQty,
			"costPrice": round2f(p.CostPrice), "lastPrice": last, "marketValue": mv,
			"pnl": pnl, "pnlPct": pnlPct, "dayPnl": dp, "dayChgPct": chgPct,
		})
	}
	total := acct.Cash + marketValue
	// 净值快照：请求即 upsert 当日（同日以最后一次为准），曲线随使用自然积累。
	_ = s.UpsertPaperEquity(r.Context(), acct.ID, store.PaperEquityPoint{
		Date: today, Cash: round2f(acct.Cash), MarketValue: round2f(marketValue), Total: round2f(total)})
	curve, err := s.PaperEquityCurve(r.Context(), acct.ID)
	if err != nil {
		return nil, err
	}
	trades, err := s.PaperTrades(r.Context(), acct.ID, 200)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"account":     acct,
		"cash":        round2f(acct.Cash),
		"marketValue": round2f(marketValue),
		"total":       round2f(total),
		"totalPnl":    round2f(total - acct.InitialCash),
		"dayPnl":      round2f(dayPnl),
		"positions":   positions,
		"trades":      trades,
		"curve":       curve,
	}, nil
}

// paperExec 下单撮合：全部校验通过才落账。
func paperExec(r *http.Request, s *store.Store, qc *quotes.Cache, ov *overlay, req paperOrderReq) (store.PaperTrade, error) {
	if req.Side != "buy" && req.Side != "sell" {
		return store.PaperTrade{}, fmt.Errorf("side 必须是 buy 或 sell")
	}
	if req.Qty <= 0 || req.Qty%100 != 0 {
		return store.PaperTrade{}, fmt.Errorf("数量须为 100 股的整数倍")
	}
	price := paperPrice(s, qc, ov, req.Symbol)
	if price <= 0 {
		return store.PaperTrade{}, fmt.Errorf("%s 无可用价格（快照与日线均缺）", req.Symbol)
	}
	acct, err := s.EnsurePaperAccount(r.Context(), paperAccountName, paperInitialCash)
	if err != nil {
		return store.PaperTrade{}, err
	}
	amount := round2f(price * float64(req.Qty))
	fee, tax := paperFees(req.Side, amount)
	if req.Side == "sell" {
		// T+1：可卖量 = 早于今日的净买入，从流水推导。
		pos, err := s.PaperRawPositions(r.Context(), acct.ID, time.Now().Format("2006-01-02"))
		if err != nil {
			return store.PaperTrade{}, err
		}
		for _, p := range pos {
			if p.Symbol == req.Symbol && req.Qty > p.AvailQty {
				return store.PaperTrade{}, fmt.Errorf("可卖不足：T+1 可卖 %d 股（总持仓 %d 股）", p.AvailQty, p.Qty)
			}
		}
	}
	trade, err := s.ExecPaperTrade(r.Context(), acct.ID, store.PaperTrade{
		Symbol: req.Symbol, Name: req.Name, Side: req.Side,
		Price: round2f(price), Qty: req.Qty, Amount: amount, Fee: fee, Tax: tax,
		Note: req.Note, Signal: req.Signal,
	})
	if err != nil {
		return trade, err
	}
	trade.TradedAt = time.Now().Format("2006-01-02 15:04:05")
	return trade, nil
}

// paperRoutes 挂 /api/paper/*。
func paperRoutes(mux *http.ServeMux, s *store.Store, qc *quotes.Cache, ov *overlay) {
	mux.HandleFunc("GET /api/paper/overview", func(w http.ResponseWriter, r *http.Request) {
		v, err := paperOverview(r, s, qc, ov)
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/paper/orders", func(w http.ResponseWriter, r *http.Request) {
		var req paperOrderReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errorJSON(w, 400, "invalid json: "+err.Error())
			return
		}
		if req.Symbol == "" {
			errorJSON(w, 400, "symbol is required")
			return
		}
		trade, err := paperExec(r, s, qc, ov, req)
		if err != nil {
			errorJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, trade)
	})
}
