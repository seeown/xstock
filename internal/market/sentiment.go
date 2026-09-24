package market

import "sort"

// 市场情绪统计：全市场日线回溯逐日涨跌停家数、炸板率、最高连板、晋级
// 率（收盘口径），叠加实时行情快照得到盘中口径。
//
// 口径说明（与行情软件略有差异）：
//   - 涨停/跌停按「涨跌幅 ≥ 板块限额 − 0.2pp」的容差判定（QFQ 复权序
//     列，见 zt.go 包注释）；历史口径不识别 ST（5% 限额），ST 的涨跌停
//     不计入、家数略偏保守；实时口径由调用方传入 ST 感知的 pctFor。
//   - 炸板：日内最高价触及涨停价但收盘未封；炸板率 = 炸板/(炸板+封板)。
//   - 连板：连续收盘涨停的天数（停牌断档自然断板）；晋级 = 昨日涨停
//     （该票上一根K线）今日再封。

// SentimentDay 一个交易日的情绪统计（收盘口径）。
type SentimentDay struct {
	Date           string  `json:"date"`
	LimitUp        int     `json:"limitUp"`
	LimitDown      int     `json:"limitDown"`
	Broke          int     `json:"broke"`
	BreakRate      float64 `json:"breakRate"`
	MaxBoards      int     `json:"maxBoards"`
	YesterdayLimit int     `json:"yesterdayLimit"`
	Promoted       int     `json:"promoted"`
	PromoteRate    float64 `json:"promoteRate"`
}

// SentimentBase 全市场扫描结果：逐日情绪序列 + 截至序列末日/前一日的
// 每票连板数（只存 ≥1 的；末日表供盘中口径，前一日表供收盘更新后的
// 连板梯队——那时日线已含今日，"昨日梯队"要用倒数第二根）。
type SentimentBase struct {
	AsOf         string
	Days         []SentimentDay
	StreakAtEnd  map[string]int
	StreakAtPrev map[string]int
}

// isLimitDown 判断 close 相对 prevClose 是否达到跌停（pct 为百分数）。
func isLimitDown(prevClose, close, pct float64) bool {
	if prevClose <= 0 || pct <= 0 {
		return false
	}
	return close/prevClose*100-100 <= -(pct - limitTolerancePct)
}

// IsDailyLimitUp 判定一根日线是否收盘涨停（server 层的板块统计用，
// 口径同 SentimentHistory）。
func IsDailyLimitUp(prevClose, close, pct float64) bool {
	return isLimitUp(prevClose, close, pct)
}

// SentimentHistory 遍历全市场日线，产出最近 days 个交易日的情绪序列。
// barsOf 返回升序日线；一次全量扫描为秒级，调用方负责缓存。
func SentimentHistory(days int, symbols []string, barsOf func(string) []Candle) SentimentBase {
	type dayAgg struct {
		limitUp, limitDown, broke, maxBoards, promoted int
	}
	agg := map[string]*dayAgg{}
	streakAtEnd := map[string]int{}
	out := SentimentBase{StreakAtEnd: streakAtEnd, StreakAtPrev: map[string]int{}}
	for _, sym := range symbols {
		bars := barsOf(sym)
		if len(bars) < 2 {
			continue
		}
		class := limitClassOf(sym)
		if class.base == 0 {
			continue // 北交所等不支持
		}
		streak, prevUp := 0, false
		prevStreak, savedPrev := 0, false
		for i := 1; i < len(bars); i++ {
			prev, cur := bars[i-1], bars[i]
			pct := class.pct(cur.Date)
			up := isLimitUp(prev.Close, cur.Close, pct)
			d := agg[cur.Date]
			if d == nil {
				d = &dayAgg{}
				agg[cur.Date] = d
			}
			if up {
				d.limitUp++
				streak++
				if streak > d.maxBoards {
					d.maxBoards = streak
				}
				if prevUp {
					d.promoted++ // 昨日涨停今日再封
				}
			} else {
				streak = 0
				if isLimitUp(prev.Close, cur.High, pct) {
					d.broke++ // 触板未封
				}
			}
			if isLimitDown(prev.Close, cur.Close, pct) {
				d.limitDown++
			}
			prevUp = up
			if i == len(bars)-2 {
				prevStreak, savedPrev = streak, true
			}
		}
		if streak > 0 {
			streakAtEnd[sym] = streak
		}
		if savedPrev && prevStreak > 0 {
			out.StreakAtPrev[sym] = prevStreak
		}
	}

	dates := make([]string, 0, len(agg))
	for d := range agg {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	if len(dates) > days {
		dates = dates[len(dates)-days:]
	}
	out.Days = make([]SentimentDay, 0, len(dates))
	if len(dates) > 0 {
		out.AsOf = dates[len(dates)-1]
	}
	byDate := map[string]int{}
	for i, d := range dates {
		byDate[d] = i
	}
	var prevLimit int
	for _, d := range dates {
		a := agg[d]
		sd := SentimentDay{
			Date: d, LimitUp: a.limitUp, LimitDown: a.limitDown,
			Broke: a.broke, MaxBoards: a.maxBoards,
			YesterdayLimit: prevLimit, Promoted: a.promoted,
		}
		if sealed := a.limitUp + a.broke; sealed > 0 {
			sd.BreakRate = float64(a.broke) / float64(sealed) * 100
		}
		if prevLimit > 0 {
			sd.PromoteRate = float64(a.promoted) / float64(prevLimit) * 100
		}
		out.Days = append(out.Days, sd)
		prevLimit = a.limitUp
	}
	return out
}

// RTQuote 实时快照的最小字段（market 包不依赖 tdx，由调用方转换）。
type RTQuote struct {
	Symbol   string
	Price    float64
	PreClose float64
	High     float64
}

// SentimentRealtime 盘中口径：用最新快照的现价/日内高点判定封板与触板，
// 连板高度 = 截至上一交易日的连板数 + 今日封板。
//
// barsIncludeToday 为真表示日线已包含快照对应的交易日（收盘后增量更
// 新完成、盘前行情仍停在上一交易日等情况），此时连板数已含当日，高度
// 直接取连板数，避免重复 +1。yesterdayLimit 为快照交易日前一日的涨停
// 家数（从历史序列取）。
type SentimentRealtime struct {
	AsOf           string  `json:"asOf"`
	UpdatedAt      string  `json:"updatedAt"`
	LimitUp        int     `json:"limitUp"`
	LimitDown      int     `json:"limitDown"`
	Touched        int     `json:"touched"` // 盘中曾触板（含已封）
	Broke          int     `json:"broke"`
	BreakRate      float64 `json:"breakRate"`
	MaxBoards      int     `json:"maxBoards"`
	YesterdayLimit int     `json:"yesterdayLimit"`
	Promoted       int     `json:"promoted"`
	PromoteRate    float64 `json:"promoteRate"`
}

func ComputeSentimentRealtime(asOf, updatedAt string, quotes []RTQuote, streakAtEnd map[string]int, pctFor func(symbol string) float64, barsIncludeToday bool, yesterdayLimit int) SentimentRealtime {
	rt := SentimentRealtime{AsOf: asOf, UpdatedAt: updatedAt, YesterdayLimit: yesterdayLimit}
	touchedTotal := 0
	for _, q := range quotes {
		if q.Price <= 0 || q.PreClose <= 0 {
			continue // 停牌/无数据
		}
		pct := pctFor(q.Symbol)
		if pct <= 0 {
			continue
		}
		touched := q.High > 0 && isLimitUp(q.PreClose, q.High, pct)
		if touched {
			touchedTotal++
		}
		switch {
		case isLimitUp(q.PreClose, q.Price, pct):
			rt.LimitUp++
			streak := streakAtEnd[q.Symbol]
			height, promoted := streak+1, streak >= 1
			if barsIncludeToday {
				height, promoted = streak, streak >= 2
				if height < 1 {
					height = 1
				}
			}
			if height > rt.MaxBoards {
				rt.MaxBoards = height
			}
			if promoted {
				rt.Promoted++
			}
		case isLimitDown(q.PreClose, q.Price, pct):
			rt.LimitDown++
		case touched:
			rt.Broke++
		}
	}
	rt.Touched = touchedTotal
	if sealed := rt.LimitUp + rt.Broke; sealed > 0 {
		rt.BreakRate = float64(rt.Broke) / float64(sealed) * 100
	}
	if yesterdayLimit > 0 {
		rt.PromoteRate = float64(rt.Promoted) / float64(yesterdayLimit) * 100
	}
	return rt
}
