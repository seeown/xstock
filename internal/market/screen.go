package market

import "math"

// 全市场股票筛查：对每只票跟踪「放量首板 → 缩量回调(B1) → 放量突破(B2)
// → 回踩不破首板涨停价(B3)」的 N 字 setup 生命周期，输出截至最新一根
// K 线仍然存活的 setup 及其当前阶段。三种买法是同一结构的三种入场战术：
//
//   - B1 回调低吸：回调不破起涨点、每日收盘 ≥ MA20、逐日量 ≤ 板日量的
//     上限、回撤不超过板日涨幅的半分位；回撤进入涨幅的 1/3~1/2 为低吸
//     区间；回调 ≥2 天后首根阳线标记企稳(★)。
//   - B2 放量突破：收盘 > 回看窗口摆动高点（含 buffer，防下跌反抽板的
//     假突破）、标准量比在区间内、收盘 > 当日 MA20。收盘越过前高但量比
//     或均线不合格的按失效处理（无量/过热突破），不留在 B1 名单里。
//   - B3 回踩企稳：突破后 1~RetestMaxDays 天内，最低价回到前高附近
//     （≤ 摆动高点 × (1+band)）视为回踩开始；期间每日收盘 ≥ 首板涨停价
//     （板日收盘，盘中跌破收盘收回有效）；回踩后首根阳线标记企稳(★)；
//     任一日收盘 < 涨停价即失效。
//
// 首板判定、量比口径与限制同 zt.go（见其包注释）。MA20 为 QFQ 收盘价
// 的 20 日简单均线；板前至少需要 20 根 K 线，上市过新的票由调用方结合
// stock_profile 的上市日期过滤。

// maPeriod 趋势过滤用的均线窗口（B1 回调与 B2 突破共用）。
const maPeriod = 20

// ScreenParams 筛查参数，默认值来自手绘图规则与实测量能区间。
type ScreenParams struct {
	// 首板（复用 zt 口径）
	BoardLookbackDays   int     `json:"boardLookbackDays"`
	BoardVolRatioMin    float64 `json:"boardVolRatioMin"`
	BoardVolRatioMax    float64 `json:"boardVolRatioMax"`
	ExcludeOneWordBoard bool    `json:"excludeOneWordBoard"`

	// 回调（B1）
	PullbackMinDays     int     `json:"pullbackMinDays"` // 企稳标记的最少回调天数
	PullbackMaxDays     int     `json:"pullbackMaxDays"` // 回调观察窗口，超时未突破即失效
	PullbackMaxPct      float64 `json:"pullbackMaxPct"`  // 相对板日高点的绝对回撤上限(%)
	B1ZoneRatioMin      float64 `json:"b1ZoneRatioMin"`  // 低吸区上沿：回撤达板日涨幅的 1/3
	B1ZoneRatioMax      float64 `json:"b1ZoneRatioMax"`  // 低吸区下沿兼硬上限：涨幅的 1/2
	PullbackVolRatioMax float64 `json:"pullbackVolRatioMax"`

	// 突破（B2）
	BreakoutBufferPct   float64 `json:"breakoutBufferPct"`
	BreakoutVolRatioMin float64 `json:"breakoutVolRatioMin"`
	BreakoutVolRatioMax float64 `json:"breakoutVolRatioMax"`

	// 回踩（B3）
	RetestBandPct float64 `json:"retestBandPct"` // 最低价 ≤ 前高 × (1+band%) 视为回踩开始
	RetestMaxDays int     `json:"retestMaxDays"` // 突破后的回踩观察窗口
}

func DefaultScreenParams() ScreenParams {
	return ScreenParams{
		BoardLookbackDays:   10,
		BoardVolRatioMin:    2.5,
		BoardVolRatioMax:    8,
		ExcludeOneWordBoard: true,
		PullbackMinDays:     2,
		PullbackMaxDays:     5,
		PullbackMaxPct:      8,
		B1ZoneRatioMin:      1.0 / 3.0,
		B1ZoneRatioMax:      0.5,
		PullbackVolRatioMax: 0.7,
		BreakoutBufferPct:   0,
		BreakoutVolRatioMin: 1.5,
		BreakoutVolRatioMax: 3,
		RetestBandPct:       3,
		RetestMaxDays:       10,
	}
}

func ValidScreenParams(p ScreenParams) error {
	if p.BoardLookbackDays < 1 || p.BoardVolRatioMin <= 0 || p.BoardVolRatioMax <= p.BoardVolRatioMin ||
		p.PullbackMinDays < 1 || p.PullbackMaxDays < p.PullbackMinDays || p.PullbackMaxPct <= 0 ||
		p.B1ZoneRatioMin <= 0 || p.B1ZoneRatioMax <= p.B1ZoneRatioMin ||
		p.PullbackVolRatioMax <= 0 || p.BreakoutVolRatioMin <= 0 || p.BreakoutVolRatioMax < p.BreakoutVolRatioMin ||
		p.RetestBandPct < 0 || p.RetestMaxDays < 1 {
		return errInvalidParams
	}
	return nil
}

// ScreenSetup 一个存活 N 字 setup 的当前快照。Stage 是达到的最远阶段：
// b1（回调中）、b2（已突破、回踩未开始）、b3（回踩已开始）；时间过滤用
// KeyDate（b1 → 首板日，b2/b3 → 突破日）。
type ScreenSetup struct {
	Symbol  string `json:"symbol"`
	Stage   string `json:"stage"`   // b1 | b2 | b3
	KeyDate string `json:"keyDate"` // 时间过滤锚点日期
	AsOf    string `json:"asOf"`    // 数据截至日

	LimitPct      float64 `json:"limitPct"`
	BoardDate     string  `json:"boardDate"`
	BoardClose    float64 `json:"boardClose"`    // 首板涨停价：B3 硬支撑
	BoardHigh     float64 `json:"boardHigh"`     // 首板日高点（低吸区基准）
	SwingHigh     float64 `json:"swingHigh"`     // 回看窗口摆动高点：B2 触发价
	RiseStart     float64 `json:"riseStart"`     // 起涨点（板前收盘）
	BoardVolRatio float64 `json:"boardVolRatio"`
	BoardVolume   float64 `json:"boardVolume"`
	MA20          float64 `json:"ma20"`        // 首板日 MA20，展示参考
	CurrentMA20   float64 `json:"currentMa20"` // B1 阶段最后一日 MA20
	CurrentClose  float64 `json:"currentClose"`

	// B1 回调
	PullbackDays     int     `json:"pullbackDays"`
	PullbackLow      float64 `json:"pullbackLow"`
	PullbackDepth    float64 `json:"pullbackDepth"`    // 回撤占板日涨幅比例(0~1)
	PullbackVolRatio float64 `json:"pullbackVolRatio"` // 回调最大单日量 / 板日量
	B1ZoneLow        float64 `json:"b1ZoneLow"`
	B1ZoneHigh       float64 `json:"b1ZoneHigh"`
	B1Triggered      bool    `json:"b1Triggered"`
	B1TriggerDate    string  `json:"b1TriggerDate,omitempty"`
	StopLossB1       float64 `json:"stopLossB1"` // 起涨点下方

	// B2 突破
	BreakoutDate      string  `json:"breakoutDate,omitempty"`
	BreakoutPrice     float64 `json:"breakoutPrice"`
	BreakoutVolRatio  float64 `json:"breakoutVolRatio"`
	DaysSinceBreakout int     `json:"daysSinceBreakout"`
	StopLossB2        float64 `json:"stopLossB2"` // 摆动高点下方

	// B3 回踩
	RetestDays        int     `json:"retestDays"` // 突破后已过交易日数
	RetestLow         float64 `json:"retestLow"`
	RetestTriggered   bool    `json:"retestTriggered"`
	RetestTriggerDate string  `json:"retestTriggerDate,omitempty"`
	StopLossB3        float64 `json:"stopLossB3"` // 首板涨停价下方
}

// ma20Series 预计算收盘价简单均线，前 maPeriod-1 根为 0（未知）。
func ma20Series(bars []Candle) []float64 {
	ma := make([]float64, len(bars))
	var sum float64
	for i := 0; i < len(bars); i++ {
		sum += bars[i].Close
		if i >= maPeriod {
			sum -= bars[i-maPeriod].Close
		}
		if i >= maPeriod-1 {
			ma[i] = sum / maPeriod
		}
	}
	return ma
}

// FindNSetups 返回截至序列末端仍存活的 N 字 setup（时序上先后出现的
// 独立形态可能不止一个）。时间过滤由调用方按 KeyDate 完成——不同股票
// 交易日历不同，窗口起点应基于各自 bars 计算。
func FindNSetups(symbol string, bars []Candle, p ScreenParams) []ScreenSetup {
	var out []ScreenSetup
	if ValidScreenParams(p) != nil || len(bars) == 0 {
		return out
	}
	class := limitClassOf(symbol)
	if class.base == 0 {
		return out
	}
	ma := ma20Series(bars)
	for board := max(p.BoardLookbackDays, maPeriod); board < len(bars); board++ {
		if !isLimitUp(bars[board-1].Close, bars[board].Close, class.pct(bars[board].Date)) {
			continue
		}
		if !aliveBoard(symbol, bars, board, class, p) {
			continue
		}
		if s, ok := walkSetup(symbol, bars, ma, board, class, p); ok {
			out = append(out, s)
		}
	}
	return out
}

// aliveBoard 判定 board（已确认收盘涨停）是否构成合格的放量首板：
// 非一字、回看窗口内无涨停、板日量比在区间。
func aliveBoard(symbol string, bars []Candle, board int, class limitClass, p ScreenParams) bool {
	if p.ExcludeOneWordBoard && oneWordBar(bars[board]) {
		return false
	}
	for i := max(board-p.BoardLookbackDays, 1); i < board; i++ {
		if isLimitUp(bars[i-1].Close, bars[i].Close, class.pct(bars[i].Date)) {
			return false
		}
	}
	vr, ok := volRatio(bars, board)
	return ok && vr >= p.BoardVolRatioMin && vr <= p.BoardVolRatioMax
}

// walkSetup 从首板 board 出发走完生命周期，返回存活 setup 快照。
// 存活定义：b1 仍处回调观察窗口内；或已突破、回踩未破首板涨停价、且
// （已企稳 或 仍在回踩观察窗口内）。
func walkSetup(symbol string, bars []Candle, ma []float64, board int, class limitClass, p ScreenParams) (ScreenSetup, bool) {
	last := len(bars) - 1
	b := bars[board]
	amp := b.High - bars[board-1].Close // 板日涨幅（含上影）
	if amp <= 0 {
		return ScreenSetup{}, false
	}
	swing := b.High
	for i := board - p.BoardLookbackDays; i < board; i++ {
		swing = math.Max(swing, bars[i].High)
	}
	s := ScreenSetup{
		Symbol: symbol, Stage: "b1", KeyDate: b.Date, AsOf: bars[last].Date,
		LimitPct: class.pct(b.Date),
		BoardDate: b.Date, BoardClose: b.Close, BoardHigh: b.High, SwingHigh: swing,
		RiseStart: bars[board-1].Close, BoardVolRatio: boardVR(bars, board),
		BoardVolume: b.Volume, MA20: ma[board],
		B1ZoneHigh:  b.High - amp*p.B1ZoneRatioMin,
		B1ZoneLow:   b.High - amp*p.B1ZoneRatioMax,
		StopLossB1:  bars[board-1].Close * 0.995,
	}
	requiredBreakout := swing * (1 + p.BreakoutBufferPct/100)

	// ---- B1 回调阶段：逐日检查结构约束；突破可发生在回调窗口内的任意
	// 一天或窗口结束的次日（与 zt 信号语义一致：回调 2~5 天 + 次日突破）。
	pullLow, maxRelVol := math.MaxFloat64, 0.0
	breakout := -1
	for d := board + 1; d <= board+p.PullbackMaxDays+1 && d <= last; d++ {
		day := bars[d]
		// 突破优先判定：突破日不受回调约束（放量、深 V 都是常态）。
		if day.Close > requiredBreakout {
			vr, ok := volRatio(bars, d)
			if ok && vr >= p.BreakoutVolRatioMin && vr <= p.BreakoutVolRatioMax && day.Close > ma[d] {
				breakout = d
				break
			}
			// 收盘已过前高但量比/均线不合格：突破失败，setup 结束。
			return s, false
		}
		if d-board > p.PullbackMaxDays {
			break // 回调窗口（含次日）耗尽仍未突破
		}
		// 回调日约束
		if ma[d] > 0 && day.Close < ma[d] {
			return s, false // 回调收盘破 MA20
		}
		if day.Low <= s.RiseStart {
			return s, false // 破起涨点
		}
		depth := b.High - day.Low
		if depth/amp > p.B1ZoneRatioMax || depth/b.High*100 > p.PullbackMaxPct {
			return s, false // 回撤超过半分位或绝对上限
		}
		if rel := day.Volume / b.Volume; rel > p.PullbackVolRatioMax {
			return s, false // 放量回调
		} else if rel > maxRelVol {
			maxRelVol = rel
		}
		pullLow = math.Min(pullLow, day.Low)
		if d-board >= p.PullbackMinDays && !s.B1Triggered && day.Close > day.Open {
			s.B1Triggered, s.B1TriggerDate = true, day.Date
		}
	}
	if breakout < 0 {
		if last <= board+p.PullbackMaxDays {
			// 序列在回调窗口内结束：形态进行中，留在 B1 观察名单。
			s.finishB1(bars, ma, last, board, pullLow, maxRelVol, amp)
			return s, true
		}
		return s, false // 窗口耗尽未突破
	}

	// ---- B2/B3 突破后：回踩阶段 ----
	brk := bars[breakout]
	s.BreakoutDate, s.BreakoutPrice = brk.Date, brk.Close
	s.BreakoutVolRatio, _ = volRatio(bars, breakout)
	s.DaysSinceBreakout = last - breakout
	s.StopLossB2 = swing * 0.995
	s.Stage = "b2"
	s.KeyDate = brk.Date
	s.finishB1(bars, ma, breakout-1, board, pullLow, maxRelVol, amp)

	retestLow, retestStarted := math.MaxFloat64, false
	for e := breakout + 1; e <= breakout+p.RetestMaxDays && e <= last; e++ {
		day := bars[e]
		if day.Close < b.Close {
			return s, false // 回踩收盘破首板涨停价
		}
		retestLow = math.Min(retestLow, day.Low)
		if !retestStarted && day.Low <= swing*(1+p.RetestBandPct/100) {
			retestStarted = true
			s.Stage = "b3"
		}
		if retestStarted && !s.RetestTriggered && day.Close > day.Open {
			s.RetestTriggered, s.RetestTriggerDate = true, day.Date
		}
		s.RetestDays = e - breakout
	}
	if retestStarted {
		s.RetestLow = retestLow
		s.StopLossB3 = b.Close * 0.995
	}
	if last < breakout+p.RetestMaxDays {
		return s, true // 仍在回踩观察窗口内
	}
	// 窗口耗尽：只有已企稳(B3 ★)的留在名单，未企稳的过期出列。
	return s, s.RetestTriggered
}

// finishB1 填充 B1 段汇总字段；end 为 B1 阶段最后一根 K 线的索引。
func (s *ScreenSetup) finishB1(bars []Candle, ma []float64, end, board int, pullLow, maxRelVol, amp float64) {
	s.PullbackDays = end - board
	if pullLow < math.MaxFloat64 {
		s.PullbackLow = pullLow
		s.PullbackDepth = (s.BoardHigh - pullLow) / amp
	}
	s.PullbackVolRatio = maxRelVol
	s.CurrentMA20 = ma[end]
	s.CurrentClose = bars[end].Close
}

// boardVR 重取板日量比（aliveBoard 已校验过可计算性）。
func boardVR(bars []Candle, board int) float64 {
	vr, _ := volRatio(bars, board)
	return vr
}

// WindowStart 返回该序列近 days 个交易日的窗口起点日期（按 KeyDate 过
// 滤用）；days <= 0 或超出序列长度返回空串（不过滤）。
func WindowStart(bars []Candle, days int) string {
	if days <= 0 || days >= len(bars) {
		return ""
	}
	return bars[len(bars)-days].Date
}
