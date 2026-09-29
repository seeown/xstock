package main

// todayOverlay：今日实时bar叠加层。全市场行情快照（每分钟）本就携带
// OHLCV 全字段——把"今日还没走完的那根K线"现场合成，锚定校验通过后
// 拼在本地日线尾部，让日K图/筛查全天实时，收盘后由落库任务写进 PG。
//
// 锚定校验（除权检测器）：本地日线最后一根收盘必须等于快照昨收。
// 相等 → 今日无除权，安全拼接；不等 → 今日除权除息，QFQ 历史整体
// 需重算，不拼（图上显示到昨日），收盘后对该票全量QFQ重拉。
//
// 回测不经过本层——只用已收盘序列，保证结果稳定可复现（用户确认）。

import (
	"sync"
	"time"

	"xstock/internal/market"
	"xstock/internal/quotes"
	"xstock/internal/store"
	"xstock/internal/tdx"
)

type overlay struct {
	mu       sync.RWMutex
	day      string                // 今日bar所属交易日（quoteDayOf 推断）
	bars     map[string]market.Candle // 锚定OK的今日bar（按代码）
	rebased  map[string]bool       // 锚定失败（今日除权）的代码集
}

func newOverlay() *overlay {
	return &overlay{bars: map[string]market.Candle{}, rebased: map[string]bool{}}
}

// refresh 用最新快照重建叠加层。幂等：任何时刻（含盘中重启后）都能
// 从单次快照完整重建今日bar，因为快照自带当日累计 OHLCV。
func (ov *overlay) refresh(s *store.Store, qc *quotes.Cache) {
	snap := qc.Snapshot()
	cal := s.Bars("000001.SH")
	if len(cal) == 0 {
		return
	}
	day, _ := quoteDayOf(cal[len(cal)-1].Date, time.Now())

	bars := make(map[string]market.Candle, len(snap))
	rebased := map[string]bool{}
	for sym, q := range snap {
		if q.Price <= 0 || q.Open <= 0 || q.PreClose <= 0 {
			continue // 停牌/无数据
		}
		// 锚定校验：本地最后一根收盘 == 快照昨收。
		hist := s.Bars(sym)
		anchored := false
		if n := len(hist); n > 0 {
			last := hist[n-1]
			// 快照属于今天而本地日线还没今天这根（正常盘中状态），
			// 或日线已含今天（落库后）都要求基准一致。
			ref := last
			if last.Date == day && n >= 2 {
				ref = hist[n-2] // 日线已含今日时与真昨收比
			}
			anchored = absF(ref.Close-q.PreClose) <= ref.Close*0.001
		}
		if !anchored {
			rebased[sym] = true
			continue
		}
		// 日线已含今日（落库完成）则不再叠（避免重复拼bar）。
		if n := len(hist); n > 0 && hist[n-1].Date == day {
			continue
		}
		bars[sym] = market.Candle{
			Date: day, Open: q.Open, High: q.High, Low: q.Low,
			Close: q.Price, Volume: q.Vol,
		}
	}
	ov.mu.Lock()
	ov.day, ov.bars, ov.rebased = day, bars, rebased
	ov.mu.Unlock()
}

// Bars 返回拼接了今日实时bar的日线序列（图表/筛查用）。
func (ov *overlay) Bars(s *store.Store, symbol string) []market.Candle {
	hist := s.Bars(symbol)
	ov.mu.RLock()
	defer ov.mu.RUnlock()
	if ov.day == "" {
		return hist
	}
	if n := len(hist); n > 0 && hist[n-1].Date >= ov.day {
		return hist // 日线已含今日（落库后/已最新）
	}
	if b, ok := ov.bars[symbol]; ok {
		return append(append([]market.Candle{}, hist...), b)
	}
	return hist
}

// pendingRebase 返回锚定失败（今日除权）的代码，收盘落库时对其全量QFQ重拉。
func (ov *overlay) pendingRebase() []string {
	ov.mu.RLock()
	defer ov.mu.RUnlock()
	out := make([]string, 0, len(ov.rebased))
	for sym := range ov.rebased {
		out = append(out, sym)
	}
	return out
}

func (ov *overlay) dayOf() string {
	ov.mu.RLock()
	defer ov.mu.RUnlock()
	return ov.day
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// exportToday 导出带代码的今日bar列表（收盘落库用）。
type symBar struct {
	sym string
	bar market.Candle
}

func (ov *overlay) exportToday() []symBar {
	ov.mu.RLock()
	defer ov.mu.RUnlock()
	out := make([]symBar, 0, len(ov.bars))
	for sym, b := range ov.bars {
		out = append(out, symBar{sym, b})
	}
	return out
}

// snapshotToday 返回（今日交易日, 今日bar数量）——落库前的就绪检查。
func (ov *overlay) snapshotToday() (string, int) {
	ov.mu.RLock()
	defer ov.mu.RUnlock()
	return ov.day, len(ov.bars)
}

var _ = tdx.Quote{} // 引用保持（字段扩展的来源类型）
