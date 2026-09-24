package main

// marketView 聚合并缓存「情绪 + 板块」两类全市场统计：一次全量日线扫描
// 产出逐日情绪、每票连板表、行业/概念板块的等权指数与逐日涨停家数，
// 按日线 asOf 指纹失效；/api/admin/reload 后强制重算。实时部分（快照
// 封板/均涨）由 handler 每次叠加，不走缓存。

import (
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xstock/internal/market"
	"xstock/internal/quotes"
	"xstock/internal/store"
	"xstock/internal/tdx"
)

// sectorWindow 等权指数与逐日涨停的回看窗口（交易日）。
const sectorWindow = 60

// SectorRow 板块一行的静态部分（收盘口径）；实时字段由 handler 覆盖。
type SectorRow struct {
	Name          string    `json:"name"`
	Count         int       `json:"count"`
	AvgChange     float64   `json:"avgChange"`     // 实时/最近收盘均涨跌幅
	LimitUp       int       `json:"limitUp"`       // 实时封板家数（快照口径）
	LastDayLimit  int       `json:"lastDayLimit"`  // 最近收盘日涨停家数
	LimitUpRecent []int     `json:"limitUpRecent"` // 近5日每日涨停家数（旧→新）
	Index         []float64 `json:"index"`         // 近60日等权指数（起点≈100）
}

type mvResult struct {
	AsOf       string
	Days       []market.SentimentDay
	Streaks    map[string]int
	StreaksPrev map[string]int
	Industries []SectorRow
	Concepts   []SectorRow
	Mainline   string
	// MainlineStreak 主线板块连续霸榜（涨停家数第一）的天数。
	MainlineStreak int
	// 量能与广度：近60个交易日（与 sectorWindow 对齐）的全市场成交额
	// 估算（成交量×收盘价，亿元）、涨/跌家数、等权平均涨幅(%)。
	Dates     []string  `json:"dates"`
	Amounts   []float64 `json:"amounts"`
	UpCounts  []int     `json:"upCounts"`
	DownCounts []int    `json:"downCounts"`
	AvgRets   []float64 `json:"avgRets"`
}

type marketView struct {
	st *store.Store

	mu  sync.Mutex
	key string
	res *mvResult
}

func newMarketView(st *store.Store) *marketView {
	return &marketView{st: st}
}

// invalidate 在数据热刷新后调用，强制下次重算。
func (mv *marketView) invalidate() {
	mv.mu.Lock()
	mv.key, mv.res = "", nil
	mv.mu.Unlock()
}

// get 返回缓存结果；日线 asOf 或股票数量变化时重算（首算秒级）。
func (mv *marketView) get() *mvResult {
	profiles := mv.st.AllProfiles()
	cal := mv.st.Bars("000001.SH")
	if len(cal) == 0 || len(profiles) == 0 {
		return &mvResult{}
	}
	asOf := cal[len(cal)-1].Date
	key := asOf + "|" + strconv.Itoa(len(profiles))
	mv.mu.Lock()
	defer mv.mu.Unlock()
	if mv.res != nil && mv.key == key {
		return mv.res
	}
	start := time.Now()
	res := mv.compute(profiles, cal)
	mv.key, mv.res = key, res
	log.Printf("市场统计已计算：%d 个交易日情绪, %d 行业 / %d 概念, 用时 %s",
		len(res.Days), len(res.Industries), len(res.Concepts), time.Since(start).Round(time.Millisecond))
	return res
}

func (mv *marketView) compute(profiles []store.Profile, cal []market.Candle) *mvResult {
	symbols := make([]string, 0, len(profiles))
	for _, p := range profiles {
		symbols = append(symbols, p.Symbol)
	}
	base := market.SentimentHistory(120, symbols, func(sym string) []market.Candle {
		return mv.st.Bars(sym)
	})

	// 交易日轴 + 每日涨停的板块累加器 + 全市场量能/广度累加器。
	days := cal[len(cal)-sectorWindow:]
	dayIdx := make(map[string]int, len(days))
	for i, d := range days {
		dayIdx[d.Date] = i
	}
	gAmt := make([]float64, len(days))  // 成交额估算（亿元/日）
	gUp := make([]int, len(days))       // 上涨家数
	gDown := make([]int, len(days))     // 下跌家数
	gRetSum := make([]float64, len(days))
	gRetCnt := make([]int, len(days))
	type secAgg struct {
		name     string
		count    int
		retSum   []float64
		retCnt   []int
		sealed   []int
	}
	ind := map[string]*secAgg{}
	con := map[string]*secAgg{}
	getAgg := func(m map[string]*secAgg, name string) *secAgg {
		a := m[name]
		if a == nil {
			a = &secAgg{name: name, retSum: make([]float64, len(days)), retCnt: make([]int, len(days)), sealed: make([]int, len(days))}
			m[name] = a
		}
		return a
	}

	for _, p := range profiles {
		if market.LimitUpPct(p.Symbol, "2026-01-01") == 0 {
			continue // 北交所等不支持涨停口径
		}
		bars := mv.st.Bars(p.Symbol)
		if len(bars) < 2 {
			continue
		}
		var aggs []*secAgg
		if p.Industry != "" {
			a := getAgg(ind, p.Industry)
			a.count++
			aggs = append(aggs, a)
		}
		for _, c := range p.Concepts {
			a := getAgg(con, c)
			a.count++
			aggs = append(aggs, a)
		}
		for i := 1; i < len(bars); i++ {
			di, ok := dayIdx[bars[i].Date]
			if !ok {
				continue
			}
			prev, cur := bars[i-1], bars[i]
			ret := 0.0
			if prev.Close > 0 {
				ret = cur.Close/prev.Close - 1
			}
			sealed := market.IsDailyLimitUp(prev.Close, cur.Close, market.LimitUpPct(p.Symbol, cur.Date))
			// 全市场量能与广度（与板块同一次遍历内累积）
			gAmt[di] += cur.Volume * cur.Close / 1e8
			if ret > 0 {
				gUp[di]++
			} else if ret < 0 {
				gDown[di]++
			}
			gRetSum[di] += ret
			gRetCnt[di]++
			for _, a := range aggs {
				a.retSum[di] += ret
				a.retCnt[di]++
				if sealed {
					a.sealed[di]++
				}
			}
		}
	}

	toRows := func(m map[string]*secAgg) []SectorRow {
		rows := make([]SectorRow, 0, len(m))
		for _, a := range m {
			row := SectorRow{Name: a.name, Count: a.count, LastDayLimit: a.sealed[len(days)-1]}
			idx := 100.0
			for di := range days {
				if a.retCnt[di] > 0 {
					idx *= 1 + a.retSum[di]/float64(a.retCnt[di])
				}
				row.Index = append(row.Index, idx)
				if di >= len(days)-5 {
					row.LimitUpRecent = append(row.LimitUpRecent, a.sealed[di])
				}
			}
			if last := a.retSum[len(days)-1]; a.retCnt[len(days)-1] > 0 {
				row.AvgChange = last / float64(a.retCnt[len(days)-1]) * 100
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].LastDayLimit != rows[j].LastDayLimit {
				return rows[i].LastDayLimit > rows[j].LastDayLimit
			}
			if rows[i].AvgChange != rows[j].AvgChange {
				return rows[i].AvgChange > rows[j].AvgChange
			}
			return rows[i].Name < rows[j].Name
		})
		return rows
	}
	industries := toRows(ind)
	concepts := toRows(con)

	// 主线：最近收盘日涨停家数第一的行业，且连续霸榜的天数。
	mainline, streak := "", 0
	if len(industries) > 0 && industries[0].LastDayLimit > 0 {
		mainline = industries[0].Name
		streak = 1
		for di := len(days) - 2; di >= 0; di-- {
			top, topN := "", 0
			for _, a := range ind {
				if a.sealed[di] > topN {
					top, topN = a.name, a.sealed[di]
				}
			}
			if top != mainline || topN == 0 {
				break
			}
			streak++
		}
	}

	dayDates := make([]string, len(days))
	avgRets := make([]float64, len(days))
	for i, d := range days {
		dayDates[i] = d.Date
		if gRetCnt[i] > 0 {
			avgRets[i] = gRetSum[i] / float64(gRetCnt[i]) * 100
		}
	}
	return &mvResult{
		AsOf: base.AsOf, Days: base.Days, Streaks: base.StreakAtEnd, StreaksPrev: base.StreakAtPrev,
		Industries: industries, Concepts: concepts,
		Mainline: mainline, MainlineStreak: streak,
		Dates: dayDates, Amounts: gAmt, UpCounts: gUp, DownCounts: gDown, AvgRets: avgRets,
	}
}

// tempScore 情绪温度分（0~100）：涨停家数 30 + 炸板率反向 20 + 晋级率 20
// + 量能 15 + 高度 15。量能入参为当日成交额/前5日均值的比值。
func tempScore(limitUp int, breakRate, promoteRate float64, amountRatio, maxBoards float64) float64 {
	score := 0.0
	score += clamp01(float64(limitUp)/120) * 30
	score += clamp01((55-breakRate)/35) * 20 // 炸板率≤20% 满，≥55% 零
	score += clamp01((promoteRate-12)/28) * 20
	score += clamp01((amountRatio-0.72)/0.43) * 15 // 0.72×缩量 → 1.15×放量
	score += clamp01((maxBoards-2)/5) * 15         // 2板 → 7板
	return math.Round(score*10) / 10
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// tempStage 温度分对应的情绪阶段。
func tempStage(score float64) string {
	switch {
	case score < 20:
		return "冰点"
	case score < 40:
		return "低迷"
	case score < 60:
		return "中性"
	case score < 80:
		return "活跃"
	default:
		return "亢奋"
	}
}

// quadrant 量价四象限：等权平均涨跌 × 量能比。
func quadrant(avgRet, amountRatio float64) string {
	vol := "缩量"
	if amountRatio >= 1.1 {
		vol = "放量"
	} else if amountRatio >= 0.9 {
		vol = "平量"
	}
	if avgRet >= 0 {
		return vol + "上涨"
	}
	return vol + "下跌"
}

// GuideHistoryDay 情绪指南历史序列的一天。
type GuideHistoryDay struct {
	Date        string  `json:"date"`
	LimitUp     int     `json:"limitUp"`
	LimitDown   int     `json:"limitDown"`
	Broke       int     `json:"broke"`
	BreakRate   float64 `json:"breakRate"`
	Amount      float64 `json:"amount"`      // 全市场成交额估算（亿）
	AmountRatio float64 `json:"amountRatio"` // /前5日均
	UpCount     int     `json:"upCount"`
	DownCount   int     `json:"downCount"`
	TempScore   float64 `json:"tempScore"`
	Stage       string  `json:"stage"`
}

// guidePayload 组装 /api/market/guide：实时温度判定 + 30 日历史
// （涨停/炸板/量能序列与温度分曲线）。
func (mv *marketView) guidePayload(qc *quotes.Cache) map[string]any {
	res := mv.get()
	snap := qc.Snapshot()
	rt := mv.sentimentRealtimeOf(res, snap, qc)

	// 今日实时量能与广度（快照精确值）
	todayAmt, upNow, downNow, cnt, retSum := 0.0, 0, 0, 0, 0.0
	for _, q := range snap {
		if q.Price <= 0 || q.PreClose <= 0 {
			continue
		}
		todayAmt += q.Amount
		if q.ChangePct > 0 {
			upNow++
		} else if q.ChangePct < 0 {
			downNow++
		}
		retSum += q.ChangePct
		cnt++
	}
	todayAmt /= 1e8
	avgRetNow := 0.0
	if cnt > 0 {
		avgRetNow = retSum / float64(cnt)
	}
	_, include := quoteDayOf(res.AsOf, time.Now())

	// 历史序列（近30日），按日期对齐量能/广度
	amountIdx := map[string]int{}
	for i, d := range res.Dates {
		amountIdx[d] = i
	}
	amountRatioOf := func(i int) float64 {
		if i <= 0 {
			return 1
		}
		var sum float64
		n := 0
		for j := i - 5; j < i && j >= 0; j++ {
			sum += res.Amounts[j]
			n++
		}
		if n == 0 || sum <= 0 {
			return 1
		}
		return res.Amounts[i] / (sum / float64(n))
	}
	hist := make([]GuideHistoryDay, 0, 30)
	days := res.Days
	if len(days) > 30 {
		days = days[len(days)-30:]
	}
	for _, d := range days {
		g := GuideHistoryDay{
			Date: d.Date, LimitUp: d.LimitUp, LimitDown: d.LimitDown,
			Broke: d.Broke, BreakRate: d.BreakRate,
		}
		if i, ok := amountIdx[d.Date]; ok {
			g.Amount = math.Round(res.Amounts[i]*10) / 10
			g.AmountRatio = amountRatioOf(i)
			g.UpCount = res.UpCounts[i]
			g.DownCount = res.DownCounts[i]
		}
		g.TempScore = tempScore(d.LimitUp, d.BreakRate, d.PromoteRate, g.AmountRatio, float64(d.MaxBoards))
		g.Stage = tempStage(g.TempScore)
		hist = append(hist, g)
	}

	// 今日（实时或定格）量能比：收盘更新前用今日快照额/昨日全天
	amountRatioNow := 1.0
	yesterdayAmt := 0.0
	if n := len(res.Amounts); n >= 2 {
		yesterdayAmt = res.Amounts[n-1-(boolToInt(include))]
	}
	if include { // 日线已含今日：直接用历史口径
		if n := len(res.Amounts); n > 0 {
			todayAmt = res.Amounts[n-1]
			amountRatioNow = amountRatioOf(n - 1)
		}
	} else if yesterdayAmt > 0 && todayAmt > 0 {
		amountRatioNow = todayAmt / yesterdayAmt
	}
	quad := quadrant(avgRetNow, amountRatioNow)
	score := tempScore(rt.LimitUp, rt.BreakRate, rt.PromoteRate, amountRatioNow, float64(rt.MaxBoards))

	realtime := map[string]any{
		"asOf": res.AsOf, "updatedAt": rt.UpdatedAt, "final": include,
		"limitUp": rt.LimitUp, "limitDown": rt.LimitDown, "broke": rt.Broke,
		"breakRate": rt.BreakRate, "maxBoards": rt.MaxBoards,
		"promoteRate": rt.PromoteRate,
		"upCount": upNow, "downCount": downNow,
		"amountToday": math.Round(todayAmt*10) / 10, "amountYesterday": math.Round(yesterdayAmt*10) / 10,
		"amountRatio": math.Round(amountRatioNow*100) / 100,
		"avgChange":   math.Round(avgRetNow*100) / 100,
		"quadrant": quad, "tempScore": score, "stage": tempStage(score),
	}
	return map[string]any{"realtime": realtime, "history": hist}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// sentimentRealtimeOf 复用情绪实时口径计算（sentimentPayload 的内部件）。
func (mv *marketView) sentimentRealtimeOf(res *mvResult, snap map[string]tdx.Quote, qc *quotes.Cache) market.SentimentRealtime {
	profiles := mv.st.AllProfiles()
	pf := pctFor(profiles)
	quotes := make([]market.RTQuote, 0, len(snap))
	for _, q := range snap {
		quotes = append(quotes, market.RTQuote{Symbol: q.Symbol, Price: q.Price, PreClose: q.PreClose, High: q.High})
	}
	_, include := quoteDayOf(res.AsOf, time.Now())
	yesterdayLimit := 0
	if n := len(res.Days); n >= 2 {
		if include {
			yesterdayLimit = res.Days[n-2].LimitUp
		} else {
			yesterdayLimit = res.Days[n-1].LimitUp
		}
	}
	return market.ComputeSentimentRealtime(res.AsOf, qc.Updated().Format("15:04:05"), quotes, res.Streaks, pf, include, yesterdayLimit)
}

// overlayRealtime 用最新快照把板块行的实时均涨/封板家数覆盖上去，
// 并重排（涨停家数优先）。rows 会被修改（调用方传入拷贝）。
func overlayRealtime(rows []SectorRow, members map[string][]string, snap map[string]tdx.Quote, pctFor func(string) float64) {
	for i := range rows {
		sum, cnt, sealed := 0.0, 0, 0
		for _, sym := range members[rows[i].Name] {
			if q, ok := snap[sym]; ok && q.Price > 0 && q.PreClose > 0 {
				sum += q.ChangePct
				cnt++
				if pct := pctFor(sym); pct > 0 && q.Price/q.PreClose*100-100 >= pct-0.2 {
					sealed++
				}
			}
		}
		if cnt > 0 {
			rows[i].AvgChange = sum / float64(cnt)
			rows[i].LimitUp = sealed
		} else {
			rows[i].LimitUp = rows[i].LastDayLimit
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].LimitUp != rows[j].LimitUp {
			return rows[i].LimitUp > rows[j].LimitUp
		}
		return rows[i].AvgChange > rows[j].AvgChange
	})
}

// pctFor 构建 ST 感知的涨停幅度函数（实时口径用；历史口径无法追溯 ST
// 状态，保持偏保守）。
func pctFor(profiles []store.Profile) func(string) float64 {
	names := make(map[string]string, len(profiles))
	for _, p := range profiles {
		names[p.Symbol] = p.Name
	}
	today := time.Now().Format("2006-01-02")
	return func(symbol string) float64 {
		if n, ok := names[symbol]; ok && strings.Contains(strings.ToUpper(n), "ST") {
			return 5
		}
		return market.LimitUpPct(symbol, today)
	}
}

// quoteDayOf 推断快照所属交易日与日线是否已包含该日：
// 当日已开盘（工作日 09:15 后）且日线 asOf 落后 → 快照属于今天、日线未
// 含；其余情况（盘前、收盘且已更新、周末）快照与 asOf 同日。
func quoteDayOf(asOf string, now time.Time) (quoteDay string, barsInclude bool) {
	today := now.Format("2006-01-02")
	weekday := now.Weekday() != time.Saturday && now.Weekday() != time.Sunday
	opened := now.Hour() > 9 || (now.Hour() == 9 && now.Minute() >= 15)
	if weekday && today > asOf && opened {
		return today, false
	}
	return asOf, true
}

// sentimentPayload 组装 /api/market/sentiment 的响应。
func (mv *marketView) sentimentPayload(qc *quotes.Cache) map[string]any {
	res := mv.get()
	profiles := mv.st.AllProfiles()
	pf := pctFor(profiles)
	snap := qc.Snapshot()
	quotes := make([]market.RTQuote, 0, len(snap))
	for _, q := range snap {
		quotes = append(quotes, market.RTQuote{Symbol: q.Symbol, Price: q.Price, PreClose: q.PreClose, High: q.High})
	}
	_, include := quoteDayOf(res.AsOf, time.Now())
	yesterdayLimit := 0
	if n := len(res.Days); n >= 2 {
		if include {
			yesterdayLimit = res.Days[n-2].LimitUp
		} else {
			yesterdayLimit = res.Days[n-1].LimitUp
		}
	}
	rt := market.ComputeSentimentRealtime(res.AsOf, qc.Updated().Format("15:04:05"), quotes, res.Streaks, pf, include, yesterdayLimit)

	// 连板梯队：昨日梯队 + 快照晋级状态（全量选手，不折叠）。
	_, includeDay := quoteDayOf(res.AsOf, time.Now())
	streaks := res.Streaks
	if includeDay {
		streaks = res.StreaksPrev
	}
	ladder := market.BuildLadder(res.AsOf, qc.Updated().Format("15:04:05"), quotes, streaks, pf, includeDay)
	names := make(map[string]string, len(profiles))
	for _, p := range profiles {
		names[p.Symbol] = p.Name
	}
	fillNames := func(list []market.LadderStock) {
		for i := range list {
			list[i].Name = names[list[i].Symbol]
		}
	}
	for i := range ladder.Tiers {
		fillNames(ladder.Tiers[i].Promoted)
		fillNames(ladder.Tiers[i].Failed)
	}
	fillNames(ladder.NewBoards)

	hist := res.Days
	if len(hist) > 30 {
		hist = hist[len(hist)-30:]
	}
	return map[string]any{"asOf": res.AsOf, "realtime": rt, "history": hist, "ladder": ladder}
}

// sectorsPayload 组装 /api/market/sectors 的响应：缓存的收盘口径 + 快照
// 实时覆盖（均涨/封板家数），行业为主、概念为辅（概念成员有 400 上限
// 截断，家数偏保守）。
func (mv *marketView) sectorsPayload(qc *quotes.Cache) map[string]any {
	res := mv.get()
	profiles := mv.st.AllProfiles()
	pf := pctFor(profiles)
	snap := qc.Snapshot()
	indMembers, conMembers := map[string][]string{}, map[string][]string{}
	for _, p := range profiles {
		if p.Industry != "" {
			indMembers[p.Industry] = append(indMembers[p.Industry], p.Symbol)
		}
		for _, c := range p.Concepts {
			conMembers[c] = append(conMembers[c], p.Symbol)
		}
	}
	ind := append([]SectorRow(nil), res.Industries...)
	con := append([]SectorRow(nil), res.Concepts...)
	overlayRealtime(ind, indMembers, snap, pf)
	overlayRealtime(con, conMembers, snap, pf)
	if len(con) > 30 {
		con = con[:30]
	}
	return map[string]any{
		"asOf": res.AsOf, "mainline": res.Mainline, "mainlineStreak": res.MainlineStreak,
		"byIndustry": ind, "byConcept": con,
	}
}
