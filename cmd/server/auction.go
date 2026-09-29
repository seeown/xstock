package main

// 竞价异动：9:25 竞价定格后的分层读数——自选承接 / 昨日梯队承接 /
// 题材点火聚类（高开≥5% 按概念聚集）/ 全市场异动榜 + 晨报三问。
// 数据来自每分钟刷新的全市场行情快照（quotes.Cache），过程不落库；
// 9:30 后首个请求或手动触发把竞价定格 + 晨报结论归档进 PG。
//
// 注意：非交易时段快照为最近收盘全天口径（gap=当日涨跌幅、amt=全天
// 成交额），页面以 phase 字段标注；竞价字段的逐票行为以交易日 9:25
// 实测为准，阈值集中在下方 cfg 常量便于校正。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"xstock/internal/quotes"
	"xstock/internal/store"
	"xstock/internal/tdx"
)

func contextBg() context.Context { return context.Background() }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// 阈值（经验值，交易日实测后校正）
const (
	aucGapUp     = 5.0  // 高开下限 %
	aucGapDown   = -5.0 // 低开下限 %
	aucGapMover  = 3.0  // 异动榜入围 |gap| 下限
	aucMinAmt    = 3e6  // 放量绝对下限：竞价额 ≥ 300 万
	aucAmtCapMin = 0.02 // 放量相对下限：竞价额 ≥ 昨日全天 2%
	aucMaxMovers = 80
	aucMaxThemes = 8
)

// 宽基/风格类概念剔除（TDX 概念名口径，尽力匹配）
var aucExcludeConcepts = []string{
	"融资融券", "转融券", "沪股通", "深股通", "MSCI", "富时罗素", "标普",
	"上证50", "沪深300", "中证500", "中证1000", "创业板综", "次新股", "注册制",
	"ST板块", "破净", "低价股", "高送转", "预盈预增", "预亏预减",
	"机构重仓", "基金重仓", "社保重仓", "QFII", "券商重仓", "信托重仓", "央企", "国企改革",
}

type AuctionMover struct {
	Symbol  string  `json:"symbol"`
	Name    string  `json:"name,omitempty"`
	Board   string  `json:"board,omitempty"`
	GapPct  float64 `json:"gapPct"`
	Amt     float64 `json:"amt"`    // 竞价成交额（元）
	AmtCap  float64 `json:"amtCap"` // 占昨日全天成交额比例；无基准为 -1
	Kind    string  `json:"kind"`   // 高开放量 / 高开缩量 / 低开放量 / 一字涨停开 …
}

type AuctionTheme struct {
	Concept string         `json:"concept"`
	Size    int            `json:"size"`  // 概念成分家数
	Count   int            `json:"count"` // 竞价高开家数
	MaxGap  float64        `json:"maxGap"`
	Stocks  []AuctionMover `json:"stocks"`
}

type AuctionLadderRow struct {
	Symbol  string  `json:"symbol"`
	Name    string  `json:"name"`
	Height  int     `json:"height"` // 昨日连板高度
	GapPct  float64 `json:"gapPct"`
	Amt     float64 `json:"amt"`
}

type AuctionResult struct {
	AsOf        string             `json:"asOf"`
	Phase       string             `json:"phase"`
	Q1          string             `json:"q1"` // 梯队承接
	Q2          string             `json:"q2"` // 主线延续
	Q3          string             `json:"q3"` // 新点火
	Stats       map[string]int     `json:"stats"`
	Movers      []AuctionMover     `json:"movers"`
	Themes      []AuctionTheme     `json:"themes"`
	Ladder      []AuctionLadderRow `json:"ladder"`
	Watch       []AuctionMover     `json:"watch"`
	ArchivedAt  string             `json:"archivedAt,omitempty"`
}

func aucExcluded(concept string) bool {
	for _, frag := range aucExcludeConcepts {
		if strings.Contains(concept, frag) {
			return true
		}
	}
	return false
}

// limitCapByBoard 涨停幅度：主板 10%、创业/科创 20%、主板 ST 5%
// （创业/科创的 ST 注册制后同为 20%，按 5% 判会大面积误判）。
func limitCapByBoard(board, name string) float64 {
	cap := 0.10
	if strings.Contains(board, "创业板") || strings.Contains(board, "科创板") {
		cap = 0.20
	}
	if cap == 0.10 && strings.Contains(strings.ToUpper(name), "ST") {
		cap = 0.05
	}
	return cap
}

func auctionPhase(now time.Time) string {
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		return "非交易时段 · 展示最近收盘快照（非竞价口径）"
	}
	hm := now.Hour()*100 + now.Minute()
	switch {
	case hm >= 915 && hm < 920:
		return "竞价申报中 · 可撤单阶段（读数仅供参考）"
	case hm >= 920 && hm < 925:
		return "竞价申报中 · 不可撤单（真实意图）"
	case hm >= 925 && hm < 930:
		return "竞价定格 · 开盘倒计时"
	case hm >= 930 && hm < 1500:
		return "盘中 · 展示当日竞价定格"
	default:
		return "非交易时段 · 展示最近快照口径"
	}
}

// computeAuction 组装竞价异动 payload。
func computeAuction(s *store.Store, mv *marketView, qc *quotes.Cache) (*AuctionResult, error) {
	snap := qc.Snapshot()
	updated := qc.Updated()
	res := mv.get()

	profiles := map[string]store.Profile{}
	for _, p := range s.AllProfiles() {
		profiles[p.Symbol] = p
	}
	conceptSize := map[string]int{}
	for _, c := range s.ConceptCounts() {
		conceptSize[c.Concept] = c.Count
	}
	watchSet, _ := s.WatchSet(context.Background())

	yesterdayAmt := func(symbol string) float64 {
		bars := s.Bars(symbol)
		if len(bars) == 0 {
			return 0
		}
		last := bars[len(bars)-1]
		return last.Volume * last.Close
	}
	yAmtCache := map[string]float64{}
	yAmtOf := func(symbol string) float64 {
		if v, ok := yAmtCache[symbol]; ok {
			return v
		}
		v := yesterdayAmt(symbol)
		yAmtCache[symbol] = v
		return v
	}

	mkMover := func(sym string, q tdx.Quote, prof store.Profile, has bool) (AuctionMover, bool) {
		if q.Price <= 0 || q.PreClose <= 0 || strings.HasSuffix(sym, ".BJ") {
			return AuctionMover{}, false
		}
		gap := (q.Price/q.PreClose - 1) * 100
		if !has {
			return AuctionMover{Symbol: sym, GapPct: gap, Amt: q.Amount, AmtCap: -1, Kind: "无档案"}, true
		}
		cap := limitCapByBoard(prof.Board, prof.Name)
		limitPrice := math.Round(q.PreClose*(1+cap)*100) / 100
		yAmt := yAmtOf(sym)
		amtCap := -1.0
		if yAmt > 0 {
			amtCap = q.Amount / yAmt
		}
		volumed := q.Amount >= aucMinAmt && (yAmt <= 0 || q.Amount >= yAmt*aucAmtCapMin)
		var kind string
		switch {
		case q.Price >= limitPrice-1e-6 && gap > 0:
			kind = "一字涨停开"
		case gap >= aucGapUp && volumed:
			kind = "高开放量"
		case gap >= aucGapUp:
			kind = "高开缩量 · 虚高警示"
		case gap <= aucGapDown && volumed:
			kind = "低开放量 · 核按钮警示"
		case gap <= aucGapDown:
			kind = "大幅低开"
		case gap >= aucGapMover || gap <= -aucGapMover:
			kind = "温和异动"
		default:
			return AuctionMover{}, false
		}
		m := AuctionMover{Symbol: sym, Name: prof.Name, Board: prof.Board, GapPct: gap, Amt: q.Amount, AmtCap: amtCap, Kind: kind}
		return m, true
	}

	// 全市场扫描：异动榜 + 点火聚类候选
	movers := make([]AuctionMover, 0, 128)
	gapUp5 := []AuctionMover{}
	limitOpen, upCnt, downCnt := 0, 0, 0
	for sym, q := range snap {
		if indexSymbols[sym] {
			continue // 大盘指数：只喂叠加层，不进个股口径统计
		}
		prof, has := profiles[sym]
		if q.Price > 0 && q.PreClose > 0 {
			if g := (q.Price/q.PreClose - 1) * 100; g > 0 {
				upCnt++
			} else if g < 0 {
				downCnt++
			}
		}
		m, ok := mkMover(sym, q, prof, has)
		if !ok {
			continue
		}
		if m.Kind == "一字涨停开" {
			limitOpen++
		}
		if m.GapPct >= aucGapUp {
			gapUp5 = append(gapUp5, m)
		}
		movers = append(movers, m)
	}
	sort.Slice(movers, func(i, j int) bool {
		return math.Abs(movers[i].GapPct) > math.Abs(movers[j].GapPct)
	})
	if len(movers) > aucMaxMovers {
		movers = movers[:aucMaxMovers]
	}

	// 题材点火：高开≥5% 按概念聚类，阈值随概念规模分档
	buckets := map[string][]AuctionMover{}
	for _, m := range gapUp5 {
		prof, ok := profiles[m.Symbol]
		if !ok {
			continue
		}
		for _, c := range prof.Concepts {
			if aucExcluded(c) {
				continue
			}
			buckets[c] = append(buckets[c], m)
		}
	}
	themes := make([]AuctionTheme, 0, 8)
	for c, list := range buckets {
		size := conceptSize[c]
		need := 8
		switch {
		case size <= 60:
			need = 3
		case size <= 150:
			need = 5
		}
		if len(list) < need {
			continue
		}
		sorted := append([]AuctionMover(nil), list...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].GapPct > sorted[j].GapPct })
		if len(sorted) > 5 {
			sorted = sorted[:5]
		}
		themes = append(themes, AuctionTheme{Concept: c, Size: size, Count: len(list), MaxGap: sorted[0].GapPct, Stocks: sorted})
	}
	sort.Slice(themes, func(i, j int) bool {
		if themes[i].Count != themes[j].Count {
			return themes[i].Count > themes[j].Count
		}
		return themes[i].MaxGap > themes[j].MaxGap
	})
	if len(themes) > aucMaxThemes {
		themes = themes[:aucMaxThemes]
	}

	// 梯队承接：昨日连板名单 × 今竞价
	ladder := make([]AuctionLadderRow, 0, 32)
	for sym, h := range res.Streaks {
		q, ok := snap[sym]
		if !ok || q.PreClose <= 0 || q.Price <= 0 {
			continue
		}
		name := sym
		if prof, has := profiles[sym]; has && prof.Name != "" {
			name = prof.Name
		}
		ladder = append(ladder, AuctionLadderRow{Symbol: sym, Name: name, Height: h,
			GapPct: (q.Price/q.PreClose - 1) * 100, Amt: q.Amount})
	}
	sort.Slice(ladder, func(i, j int) bool {
		if ladder[i].Height != ladder[j].Height {
			return ladder[i].Height > ladder[j].Height
		}
		return ladder[i].GapPct > ladder[j].GapPct
	})
	if len(ladder) > 30 {
		ladder = ladder[:30]
	}
	ladderUp := 0
	for _, r := range ladder {
		if r.GapPct > 0 {
			ladderUp++
		}
	}

	// 自选承接
	watch := make([]AuctionMover, 0, 8)
	for sym := range watchSet {
		q, ok := snap[sym]
		if !ok {
			continue
		}
		if m, ok2 := mkMover(sym, q, profiles[sym], true); ok2 {
			watch = append(watch, m)
		} else {
			// 非异动的自选也要展示（承接视角），补一条平开记录
			gap := 0.0
			if q.PreClose > 0 && q.Price > 0 {
				gap = (q.Price/q.PreClose - 1) * 100
			}
			name := sym
			if prof, has := profiles[sym]; has && prof.Name != "" {
				name = prof.Name
			}
			watch = append(watch, AuctionMover{Symbol: sym, Name: name, GapPct: gap, Amt: q.Amount, AmtCap: -1, Kind: "平稳"})
		}
	}
	sort.Slice(watch, func(i, j int) bool { return watch[i].GapPct > watch[j].GapPct })

	// 晨报三问
	q1 := "昨日无连板梯队，无承接问题"
	if n := len(ladder); n > 0 {
		rate := float64(ladderUp) / float64(n) * 100
		tone := "中性"
		if rate >= 60 {
			tone = "承接强"
		} else if rate <= 30 {
			tone = "核按钮风险"
		}
		top := ladder[0]
		q1 = fmt.Sprintf("昨%d板 %s 竞价 %+.1f%% · 梯队高开 %d/%d（%s）", top.Height, top.Name, top.GapPct, ladderUp, n, tone)
	}
	q2 := "竞价无点火概念，等待盘中确认主线"
	if len(themes) > 0 {
		t := themes[0]
		q2 = fmt.Sprintf("竞价最强概念「%s」：%d 只高开（最高 %+.1f%%）", t.Concept, t.Count, t.MaxGap)
		if res.Mainline != "" {
			q2 += " · 昨日主线：" + res.Mainline
		}
	} else if res.Mainline != "" {
		q2 = "竞价未见聚集点火 · 昨日主线 " + res.Mainline + " 待盘中验证"
	}
	q3 := "暂无新点火概念"
	if len(themes) > 0 {
		names := make([]string, 0, len(themes))
		for i, t := range themes {
			if i >= 3 {
				break
			}
			names = append(names, t.Concept)
		}
		q3 = fmt.Sprintf("%d 个概念竞价点火：%s", len(themes), strings.Join(names, "、"))
	}

	stats := map[string]int{
		"gapUp5":   len(gapUp5),
		"limitOpen": limitOpen,
		"themes":   len(themes),
		"ladder":   len(ladder),
		"ladderUp": ladderUp,
		"upCnt":    upCnt,
		"downCnt":  downCnt,
	}

	return &AuctionResult{
		AsOf:   updated.Format("2006-01-02 15:04"),
		Phase:  auctionPhase(time.Now()),
		Q1:     q1, Q2: q2, Q3: q3,
		Stats:  stats,
		Movers: movers, Themes: themes, Ladder: ladder, Watch: watch,
	}, nil
}

// archiveAuction 落库竞价定格 + 晨报（幂等）。
func archiveAuction(s *store.Store, mv *marketView, qc *quotes.Cache) (string, error) {
	res, err := computeAuction(s, mv, qc)
	if err != nil {
		return "", err
	}
	date := time.Now().Format("2006-01-02")
	snap := qc.Snapshot()
	bars := make([]store.AuctionBar, 0, len(snap))
	for sym, q := range snap {
		if q.Price <= 0 || q.PreClose <= 0 || strings.HasSuffix(sym, ".BJ") {
			continue
		}
		bars = append(bars, store.AuctionBar{Symbol: sym, Open: q.Price, PreClose: q.PreClose, AuctionAmt: q.Amount})
	}
	ctx := contextBg()
	if err := s.ArchiveAuctionBars(ctx, date, bars); err != nil {
		return date, err
	}
	payload, err := jsonMarshal(res)
	if err != nil {
		return date, err
	}
	if err := s.SaveAuctionReport(ctx, date, payload); err != nil {
		return date, err
	}
	log.Printf("竞价归档完成：%s，%d 股定格", date, len(bars))
	return date, nil
}
