package market

import "sort"

// 连板梯队：按昨日梯队高度分层，给出每档的晋级率与晋级/失败的全部
// 选手（用户要求全量显示、不折叠）。盘中口径下「晋级」= 当前价格封板，
// 「失败」= 当前未封（仍可能回封）；收盘更新后为定格结果。
//
// Final 为真表示快照对应的交易日已写入日线（收盘定格）；此时昨日梯队
// 用 StreakAtPrev（倒数第二根K线的连板数）。

// LadderStock 梯队里的一只票；Name 由 server 层补。
type LadderStock struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Height    int     `json:"height"`    // 当前所处高度（晋级后 = N+1）
	ChangePct float64 `json:"changePct"` // 快照实时涨跌幅（无行情=0，多为止牌）
}

// LadderTier 昨日 N 板的一档。
type LadderTier struct {
	Height      int           `json:"height"`
	Total       int           `json:"total"` // 昨日 N 板家数
	Promoted    []LadderStock `json:"promoted"`
	Failed      []LadderStock `json:"failed"`
	PromoteRate float64       `json:"promoteRate"`
}

type Ladder struct {
	AsOf      string        `json:"asOf"` // 昨日（梯队基准日）
	UpdatedAt string        `json:"updatedAt"`
	Final     bool          `json:"final"`
	Tiers     []LadderTier  `json:"tiers"` // 高度从高到低
	NewBoards []LadderStock `json:"newBoards"` // 今日新晋首板（昨日无板今日封）
}

// BuildLadder 由「昨日连板表 + 实时快照」拼出梯队视图，无需再扫日线。
func BuildLadder(asOf, updatedAt string, quotes []RTQuote, streakYesterday map[string]int, pctFor func(symbol string) float64, final bool) Ladder {
	type status struct {
		sealed bool
		chg    float64
	}
	today := make(map[string]status, len(quotes))
	for _, q := range quotes {
		if q.Price <= 0 || q.PreClose <= 0 {
			continue
		}
		pct := pctFor(q.Symbol)
		if pct <= 0 {
			continue
		}
		today[q.Symbol] = status{
			sealed: isLimitUp(q.PreClose, q.Price, pct),
			chg:    (q.Price/q.PreClose - 1) * 100,
		}
	}

	lad := Ladder{AsOf: asOf, UpdatedAt: updatedAt, Final: final}
	tiers := map[int]*LadderTier{}
	tier := func(h int) *LadderTier {
		t := tiers[h]
		if t == nil {
			t = &LadderTier{Height: h}
			tiers[h] = t
		}
		return t
	}
	for sym, h := range streakYesterday {
		t := tier(h)
		t.Total++
		st, ok := today[sym]
		if ok && st.sealed {
			t.Promoted = append(t.Promoted, LadderStock{Symbol: sym, Height: h + 1, ChangePct: st.chg})
		} else {
			chg := 0.0
			if ok {
				chg = st.chg
			}
			t.Failed = append(t.Failed, LadderStock{Symbol: sym, Height: h, ChangePct: chg})
		}
	}
	for sym, st := range today {
		if !st.sealed || streakYesterday[sym] > 0 {
			continue
		}
		lad.NewBoards = append(lad.NewBoards, LadderStock{Symbol: sym, Height: 1, ChangePct: st.chg})
	}

	for _, t := range tiers {
		if t.Total > 0 {
			t.PromoteRate = float64(len(t.Promoted)) / float64(t.Total) * 100
		}
		// 失败的按跌幅从小到大（接近回封的排前面），晋级按代码序稳定展示。
		sort.Slice(t.Failed, func(i, j int) bool { return t.Failed[i].ChangePct > t.Failed[j].ChangePct })
		sort.Slice(t.Promoted, func(i, j int) bool { return t.Promoted[i].Symbol < t.Promoted[j].Symbol })
		if len(t.Promoted) == 0 && len(t.Failed) == 0 {
			continue
		}
		lad.Tiers = append(lad.Tiers, *t)
	}
	sort.Slice(lad.Tiers, func(i, j int) bool { return lad.Tiers[i].Height > lad.Tiers[j].Height })
	sort.Slice(lad.NewBoards, func(i, j int) bool { return lad.NewBoards[i].ChangePct > lad.NewBoards[j].ChangePct })
	return lad
}
