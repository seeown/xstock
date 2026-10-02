package main

// 模拟仓：A股规则口径的纸面交易——市价单即时撮合（盘中用实时快照价，
// 快照缺失回落日线最后收盘）、100 股整数手、T+1（可卖量从流水推导）、
// 佣金(万2.5 最低5元)+过户费(万0.1)双边、印花税(千0.5)卖出。
// 费率做成包级常量，口径调整改这里。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
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

// paperQuoteToday 估值口径三件套：现价、昨收、是否有"今日"行情。
// 判定链（自洽于所有场景——盘中/收盘后/假期定格/次日开盘前）：
//  1. 日线含今日 bar（叠加层实时拼出或收盘已落库）→ 有今日行情，
//     昨收 = 倒数第二根收盘；
//  2. 否则看快照：快照昨收 == 最近落库收盘 才是"活的今日盘中"
//     （真交易日的昨收口径就是上一交易日收盘）；对不上说明快照是
//     休市定格（通达信在假期返回上个交易日的快照，其昨收是上上个
//     交易日）→ 无今日行情，今日盈亏/当日涨幅必须为 0。
func paperQuoteToday(s *store.Store, qc *quotes.Cache, ov *overlay, symbol, today string) (price, preClose float64, hasToday bool) {
	snap := qc.Snapshot()
	q, hasSnap := snap[symbol]
	if hasSnap && q.Price <= 0 {
		hasSnap = false
	}
	bars := ov.Bars(s, symbol)
	if len(bars) >= 2 && bars[len(bars)-1].Date == today {
		p := bars[len(bars)-1].Close
		if hasSnap {
			p = q.Price // 盘中实时价优先于叠加 bar
		}
		return p, bars[len(bars)-2].Close, true
	}
	if hasSnap {
		if len(bars) > 0 && math.Abs(q.PreClose-bars[len(bars)-1].Close) <= 0.011 {
			return q.Price, q.PreClose, true
		}
		return q.Price, q.Price, false // 定格快照：价格可用，但没有今日行情
	}
	if len(bars) > 0 {
		return bars[len(bars)-1].Close, bars[len(bars)-1].Close, false
	}
	return 0, 0, false
}

// paperPrice 服务端定价（下单用，只要一个可用价格）：快照优先，
// 回落日线最后收盘。
func paperPrice(s *store.Store, qc *quotes.Cache, ov *overlay, symbol string) float64 {
	price, _, _ := paperQuoteToday(s, qc, ov, symbol, time.Now().Format("2006-01-02"))
	return price
}

type paperOrderReq struct {
	Symbol     string          `json:"symbol"`
	Name       string          `json:"name"`
	Side       string          `json:"side"` // buy | sell
	Qty        int             `json:"qty"`
	Note       string          `json:"note"`
	Signal     json.RawMessage `json:"signal"` // 下单时的雷达信号上下文
	Limit      bool            `json:"limit"`      // 限价单
	LimitPrice float64         `json:"limitPrice"` // 限价
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
		last, preClose, hasToday := paperQuoteToday(s, qc, ov, p.Symbol, today)
		mv := round2f(last * float64(p.Qty))
		marketValue += mv
		cost := round2f(p.CostPrice * float64(p.Qty))
		pnl := round2f(mv - cost)
		pnlPct := 0.0
		if cost > 0 {
			pnlPct = round2f(mv/cost*100 - 100)
		}
		// 当日盈亏/当日涨幅：没有今日行情（假期、次日开盘前快照定格）
		// 一律为 0——昨日涨幅不属于今天，更不属于收盘后才建仓的持仓。
		// 隔夜仓 = 数量 × (现价 − 昨收) 直算；今日买入部分从成交价（含
		// 费）起算。恒等式验收：各日盈亏之和 = 累计盈亏。
		chgPct, dp := 0.0, 0.0
		if hasToday && preClose > 0 {
			chgPct = round2f((last/preClose - 1) * 100)
			if overnight := p.Qty - p.TodayQty; overnight > 0 {
				dp += (last - preClose) * float64(overnight)
			}
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
		// 挂单一并返回（含近期已了结的）。
		acct := v["account"].(store.PaperAccount)
		open, _ := s.PaperOpenOrders(r.Context(), acct.ID, "")
		v["orders"] = open
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
		// 限价单：挂起等待撮合（买卖方向与现价交叉时也照挂——行为贴近
		// 券商限价单，下一轮撮合循环触价即成）。
		if req.Limit && req.LimitPrice > 0 {
			acct, err := s.EnsurePaperAccount(r.Context(), paperAccountName, paperInitialCash)
			if err != nil {
				errorJSON(w, 500, err.Error())
				return
			}
			o, err := s.CreatePaperOrder(r.Context(), acct.ID, store.PaperOpenOrder{
				Symbol: req.Symbol, Name: req.Name, Side: req.Side,
				Qty: req.Qty, LimitPrice: req.LimitPrice, Note: req.Note, Signal: req.Signal,
			})
			if err != nil {
				errorJSON(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, map[string]any{"placed": o})
			return
		}
		trade, err := paperExec(r, s, qc, ov, req)
		if err != nil {
			errorJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"filled": trade})
	})
	mux.HandleFunc("DELETE /api/paper/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			errorJSON(w, 400, "invalid id")
			return
		}
		acct, err := s.EnsurePaperAccount(r.Context(), paperAccountName, paperInitialCash)
		if err != nil {
			errorJSON(w, 500, err.Error())
			return
		}
		if err := s.CancelPaperOrder(r.Context(), acct.ID, id); err != nil {
			errorJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
}

// paperLimitMatch 限价撮合：快照价触到即成交（买入 ≤ 限价、卖出 ≥ 限价）。
// 校验失败（现金/T+1）保持挂单下轮再试——挂单天然等现金到账或 T+1 解冻。
// 由 main 的 goroutine 周期调用。
func paperLimitMatch(s *store.Store, qc *quotes.Cache, ov *overlay) {
	ctx := context.Background()
	orders, err := s.OpenOrdersWithAccount(ctx)
	if err != nil || len(orders) == 0 {
		return
	}
	for _, o := range orders {
		price := paperPrice(s, qc, ov, o.Symbol)
		if price <= 0 {
			continue
		}
		hit := (o.Side == "buy" && price <= o.LimitPrice) || (o.Side == "sell" && price >= o.LimitPrice)
		if !hit {
			continue
		}
		amount := round2f(price * float64(o.Qty))
		fee, tax := paperFees(o.Side, amount)
		trade, err := s.MatchPaperOrder(ctx, o.AccountID, o.ID, store.PaperTrade{
			Symbol: o.Symbol, Name: o.Name, Side: o.Side,
			Price: round2f(price), Qty: o.Qty, Amount: amount, Fee: fee, Tax: tax,
			Note: mergeNote(o.Note, o.LimitPrice), Signal: o.Signal,
		}, price)
		if err != nil {
			log.Printf("模拟仓限价单 %d 未成（%s %s %d股 限价%.2f 现价%.2f）: %v",
				o.ID, o.Side, o.Symbol, o.Qty, o.LimitPrice, price, err)
			continue
		}
		log.Printf("模拟仓限价单成交：%s %s %d 股 @%.2f（限价 %.2f）",
			o.Side, o.Symbol, o.Qty, trade.Price, o.LimitPrice)
	}
}

func mergeNote(note string, limit float64) string {
	if note != "" {
		note += " · "
	}
	return note + fmt.Sprintf("限价单 @%.2f", limit)
}
