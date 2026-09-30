package main

// 形态归档：每日筛查结果的定格存档与后续走势跟踪。
// 「立即归档」= 服务端现扫全市场（与 /api/screen 同口径，含今日实时bar
// 叠加层），以数据基准日 asOf 为归档日全量替换入库；「查看归档」按日
// 取回，T+1/T+5/T+10 走势从买点日（B1=企稳触发日，B2=突破日，B3=回踩
// 确认日）的收盘价动态计算——日线库里都有，不落库。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"xstock/internal/market"
	"xstock/internal/store"
)

type screenItem struct {
	market.NPSetup
	Name     string `json:"name"`
	Industry string `json:"industry,omitempty"`
	sortKey  string
}

// radarParams 读取「雷达默认」参数组（strategy=radar 槽位）：从内置值起步
// 做部分覆盖，JSON 里没出现的字段保留内置值；未配置时就是内置默认。
func radarParams(ctx context.Context, s *store.Store) market.NPParams {
	p := market.DefaultNPParams()
	ps, err := s.DefaultParamSet(ctx, "radar")
	if err != nil {
		log.Printf("读取雷达默认参数组失败，使用内置值: %v", err)
		return p
	}
	if ps == nil {
		return p
	}
	if err := json.Unmarshal(ps.Params, &p); err != nil {
		log.Printf("雷达默认参数组 %s 解析失败，使用内置值: %v", ps.Name, err)
		return market.DefaultNPParams()
	}
	return p
}

// scanScreen 全市场 N 字筛查（GET /api/screen 的本体，归档复用同口径）。
// 参数由调用方传入（「雷达默认」参数组），不再写死内置值。
func scanScreen(s *store.Store, ov *overlay, days int, params market.NPParams) (asOf string, counts map[string]int, items []screenItem) {
	// 恐慌日集合：从上证指数日线计算（单日跌幅 ≥1.5%）。
	panicDays := map[string]bool{}
	if idx := s.Bars("000001.SH"); len(idx) > 1 {
		for i := 1; i < len(idx); i++ {
			if prev := idx[i-1].Close; prev > 0 && (idx[i].Close/prev-1)*100 <= -1.5 {
				panicDays[idx[i].Date] = true
			}
		}
	}
	isPanic := func(date string) bool { return panicDays[date] }

	items = make([]screenItem, 0, 64)
	counts = map[string]int{"b1": 0, "b2": 0, "b3": 0}
	for _, info := range s.Symbols() {
		sym := info.Symbol
		bars := ov.Bars(s, sym) // 含今日实时bar：盘中归档也能定格当日正在形成的形态
		if len(bars) < 60 {
			continue // 上市过新：MA20/量比前置不足
		}
		prof, hasProf := s.Profile(sym)
		if hasProf && strings.Contains(strings.ToUpper(prof.Name), "ST") {
			continue // ST 5% 限额无法按日线识别，整体排除
		}
		if strings.HasSuffix(sym, ".BJ") {
			continue // 北交所：流动性口径外，排除
		}
		start := market.WindowStart(bars, days)
		for _, setup := range market.FindNPatterns(sym, bars, params, isPanic) {
			key := setup.KeyDate
			if setup.Stage == "b1" {
				key = setup.B1TriggerDate
			}
			if start != "" && key < start {
				continue
			}
			counts[setup.Stage]++
			it := screenItem{NPSetup: setup, sortKey: key}
			if hasProf {
				it.Name, it.Industry = prof.Name, prof.Industry
			}
			items = append(items, it)
			if setup.AsOf > asOf {
				asOf = setup.AsOf
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].sortKey != items[j].sortKey {
			return items[i].sortKey > items[j].sortKey
		}
		return items[i].Symbol < items[j].Symbol
	})
	return asOf, counts, items
}

// buyPointOf 买点日：B1=企稳触发日，B2=突破日，B3=回踩确认日（后两者即 KeyDate）。
func buyPointOf(setup market.NPSetup) string {
	if setup.Stage == "b1" && setup.B1TriggerDate != "" {
		return setup.B1TriggerDate
	}
	return setup.KeyDate
}

func round2(v float64) *float64 {
	r := float64(int(v*100+0.5)) / 100 // 四舍五入到两位，避免浮点尾数
	if v < 0 {
		r = float64(int(v*100-0.5)) / 100
	}
	return &r
}

// perfFromBuy 从日线计算买点后的走势：T+N 收盘收益%、期间最高收益%。
// 基准 = 买点日收盘价；窗口未走完（未来）为 nil。
func perfFromBuy(bars []market.Candle, buyDate string) (buyPrice, t1, t5, t10, hi *float64) {
	idx := -1
	for i, b := range bars {
		if b.Date == buyDate {
			idx = i
			break
		}
	}
	if idx < 0 || bars[idx].Close <= 0 {
		return nil, nil, nil, nil, nil
	}
	buy := bars[idx].Close
	buyPrice = round2(buy)
	at := func(n int) *float64 {
		if idx+n >= len(bars) {
			return nil
		}
		return round2((bars[idx+n].Close/buy - 1) * 100)
	}
	t1, t5, t10 = at(1), at(5), at(10)
	maxHigh := 0.0
	for i := idx; i < len(bars); i++ {
		if bars[i].High > maxHigh {
			maxHigh = bars[i].High
		}
	}
	if maxHigh > 0 {
		hi = round2((maxHigh/buy - 1) * 100)
	}
	return
}

// archiveScreenNow 立即归档：现扫 + 以 asOf 全量替换。
func archiveScreenNow(r *http.Request, s *store.Store, ov *overlay) (map[string]any, error) {
	rp := radarParams(r.Context(), s)
	asOf, _, items := scanScreen(s, ov, 10, rp)
	if asOf == "" {
		return nil, fmt.Errorf("扫描结果为空（本地日线为空？先同步日K数据）")
	}
	paramsJSON, err := json.Marshal(rp)
	if err != nil {
		return nil, err
	}
	rows := make([]store.ScreenSnapshot, 0, len(items))
	seenKey := map[string]bool{}
	for _, it := range items {
		setupJSON, err := json.Marshal(it.NPSetup)
		if err != nil {
			return nil, err
		}
		// 同一 (symbol, stage, key_date) 只归档一条：不同 A 段入口的路径
		// 可能在同一突破日汇合，键重复在唯一约束下会炸整批写入。
		k := it.Symbol + "|" + it.Stage + "|" + it.KeyDate
		if seenKey[k] {
			continue
		}
		seenKey[k] = true
		rows = append(rows, store.ScreenSnapshot{
			Date: asOf, Stage: it.Stage, Symbol: it.Symbol, KeyDate: it.KeyDate,
			Name: it.Name, Industry: it.Industry, Setup: setupJSON,
		})
	}
	if err := s.ReplaceScreenSnapshots(r.Context(), asOf, paramsJSON, rows); err != nil {
		return nil, err
	}
	// counts 从去重后的 rows 重算，与 total 一致（items 可能含重复键）。
	archCounts := map[string]int{"b1": 0, "b2": 0, "b3": 0}
	for _, r := range rows {
		archCounts[r.Stage]++
	}
	return map[string]any{"date": asOf, "counts": archCounts, "total": len(rows)}, nil
}

// archivedScreenItem 归档视图的一行：形态全量 + 买点与后续走势。
type archivedScreenItem struct {
	market.NPSetup
	Name     string   `json:"name,omitempty"`
	Industry string   `json:"industry,omitempty"`
	BuyDate  string   `json:"buyDate"`
	BuyPrice *float64 `json:"buyPrice"`
	T1       *float64 `json:"t1"`
	T5       *float64 `json:"t5"`
	T10      *float64 `json:"t10"`
	Hi       *float64 `json:"hi"`
}

// archivedScreenOfDay 取某日归档并叠加动态走势。
func archivedScreenOfDay(r *http.Request, s *store.Store, ov *overlay, date string) ([]archivedScreenItem, map[string]int, error) {
	snapshots, err := s.ScreenSnapshotsOfDay(r.Context(), date)
	if err != nil {
		return nil, nil, err
	}
	out := make([]archivedScreenItem, 0, len(snapshots))
	counts := map[string]int{"b1": 0, "b2": 0, "b3": 0}
	for _, snap := range snapshots {
		var setup market.NPSetup
		if err := json.Unmarshal(snap.Setup, &setup); err != nil {
			continue // 脏数据跳过，不拖垮整个视图
		}
		buyDate := buyPointOf(setup)
		buyPrice, t1, t5, t10, hi := perfFromBuy(ov.Bars(s, snap.Symbol), buyDate)
		counts[setup.Stage]++
		out = append(out, archivedScreenItem{
			NPSetup: setup, Name: snap.Name, Industry: snap.Industry,
			BuyDate: buyDate, BuyPrice: buyPrice, T1: t1, T5: t5, T10: t10, Hi: hi,
		})
	}
	return out, counts, nil
}
