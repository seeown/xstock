package market

import "math"

// 按手册重构的 N 字筛查（替代旧的「首板」状态机 screen.go）：
//
//	A段(S1)：1~2 根放量阳线，量 ≥ 前期 5 日均量×2，段涨幅 10~25%，
//	         A 段前 20 日累计涨幅 ≤15%（S6：防高位滞涨区的二次冲高）。
//	         涨停基因为展示加分项（RequireLimitUpA 可强制）。
//	B段(S2~S5)：3~10 个交易日，回撤至 A 段涨幅的 0.382~0.618
//	         （0.382~0.5 为黄金低吸区，>0.618 作废）；阴线日量 ≤ A 段
//	         均量×60%（放量下跌=出货，作废）；收盘 ≥ MA20、低点高于
//	         A 段起点（S4，破任一作废）。
//	买点① B1：回撤进入黄金区 且 出现企稳信号（长下影/阳包阴/地量后
//	         放量阳/20日线首阳，任一）。
//	买点② B2：B 段 ≥3 日后收盘 ≥ 颈线×1.01 且 量 ≥ B 段均量×2 且
//	         非大盘恐慌日（指数单日跌幅 ≥1.5%，由调用方注入）。
//	         收盘过颈线但量不足 → 无量突破（F-2），出局不作信号。
//	B3 回踩：突破后 3 日内 low 回到颈线 ×1.02 以内为回踩，收盘不破
//	         颈线；回踩日量 ≤ B 段均量×80% 视为缩量确认（★）。
//	         3 日内收盘跌回颈线下方 → 假突破（F-3），出局。
//	铁律止损锚 = B 段最低点；目标位 C≈A = B低 + A段涨幅（展示参考）。
//	追高禁令：现价高于颈线超过 A 段涨幅一半，或高出 5 日线 8% → 标记。

// NPParams 手册口径参数（日线默认值，可按周期平移）。
type NPParams struct {
	// S1 A段
	ARiseMinPct     float64 `json:"aRiseMinPct"`     // 10
	ARiseMaxPct     float64 `json:"aRiseMaxPct"`     // 25
	AMaxBars        int     `json:"aMaxBars"`        // 2（1~2 根）
	AVolRatio       float64 `json:"aVolRatio"`       // 2（≥前期5日均量×2）
	CalmLookback    int     `json:"calmLookback"`    // 20（S6 前期观察窗）
	CalmMaxPct      float64 `json:"calmMaxPct"`      // 15（前期累计涨幅上限）
	RequireLimitUpA bool    `json:"requireLimitUpA"` // A 段须含涨停（默认关）

	// S2~S5 B段
	BMinDays   int     `json:"bMinDays"`   // 3
	BMaxDays   int     `json:"bMaxDays"`   // 10
	RetrMin    float64 `json:"retrMin"`    // 0.382
	GoldenMax  float64 `json:"goldenMax"`  // 0.5 黄金低吸区上界
	RetrMax    float64 `json:"retrMax"`    // 0.618 超过作废
	BVolRatio  float64 `json:"bVolRatio"`  // 0.6 阴线日量上限（×A段均量）

	// 买点②
	BreakBufPct float64 `json:"breakBufPct"` // 1（收盘≥颈线1%）
	CVolRatio   float64 `json:"cVolRatio"`   // 2（≥B段均量×2）

	// B3 回踩
	RetestDays     int     `json:"retestDays"`     // 3
	RetestBandPct  float64 `json:"retestBandPct"`  // 2（low≤颈线×1.02）
	RetestVolRatio float64 `json:"retestVolRatio"` // 0.8（缩量确认）

	// 追高禁令
	ChaseCRatio float64 `json:"chaseCRatio"` // 0.5（C段涨幅超A段一半）
	ChaseMA5Pct float64 `json:"chaseMa5Pct"` // 8（高出5日线8%）
}

func DefaultNPParams() NPParams {
	return NPParams{
		ARiseMinPct: 10, ARiseMaxPct: 25, AMaxBars: 2, AVolRatio: 2,
		CalmLookback: 20, CalmMaxPct: 15,
		BMinDays: 3, BMaxDays: 10,
		RetrMin: 0.382, GoldenMax: 0.5, RetrMax: 0.618, BVolRatio: 0.6,
		BreakBufPct: 1, CVolRatio: 2,
		RetestDays: 3, RetestBandPct: 2, RetestVolRatio: 0.8,
		ChaseCRatio: 0.5, ChaseMA5Pct: 8,
	}
}

func ValidNPParams(p NPParams) error {
	if p.AMaxBars < 1 || p.AMaxBars > 5 || p.AVolRatio <= 1 || p.ARiseMinPct <= 0 || p.ARiseMinPct > p.ARiseMaxPct ||
		p.BMinDays < 1 || p.BMaxDays < p.BMinDays || p.BVolRatio <= 0 || p.BVolRatio > 1 ||
		p.RetrMin <= 0 || p.GoldenMax < p.RetrMin || p.RetrMax < p.GoldenMax ||
		p.BreakBufPct < 0 || p.CVolRatio <= 1 || p.RetestDays < 1 || p.ChaseCRatio <= 0 || p.ChaseMA5Pct <= 0 {
		return errInvalidParams
	}
	return nil
}

// NPSetup 一个存活 N 字形态的当前快照。
type NPSetup struct {
	Symbol  string `json:"symbol"`
	Stage   string `json:"stage"`   // b1 | b2 | b3
	KeyDate string `json:"keyDate"` // b1→企稳日；b2/b3→突破日
	AsOf    string `json:"asOf"`

	// A段
	AStartDate string  `json:"aStartDate"`
	AEndDate   string  `json:"aEndDate"`
	ARisePct   float64 `json:"aRisePct"`   // (颈线/起点-1)×100
	AVolRatio  float64 `json:"aVolRatio"`  // A段量/前期5日均量
	HasLimitUp bool    `json:"hasLimitUp"` // 涨停基因（加分项）
	Neckline   float64 `json:"neckline"`   // 颈线=A段最高
	AStart     float64 `json:"aStart"`     // A段起点（前收）

	// B段
	BDays      int     `json:"bDays"`
	RetrRatio  float64 `json:"retrRatio"` // 当前回撤/A段涨幅
	BLow       float64 `json:"bLow"`      // B段最低 = 铁律止损锚
	BVolRatio  float64 `json:"bVolRatio"` // B段均量/A段均量
	Retr382    float64 `json:"retr382"`   // 黄金低吸区上沿价
	Retr50     float64 `json:"retr50"`    // 黄金低吸区下沿价
	MA20       float64 `json:"ma20"`      // 当前 MA20（展示）
	CurrentClose float64 `json:"currentClose"`

	// 买点①
	B1Triggered   bool   `json:"b1Triggered"`
	B1TriggerDate string `json:"b1TriggerDate,omitempty"`
	B1Signal      string `json:"b1Signal,omitempty"` // 触发的企稳信号类型

	// 买点②
	BreakoutDate     string  `json:"breakoutDate,omitempty"`
	BreakoutPrice    float64 `json:"breakoutPrice"`
	BreakoutVolRatio float64 `json:"breakoutVolRatio"` // 突破量/B段均量
	DaysSinceBreakout int   `json:"daysSinceBreakout"`

	// B3 回踩
	RetestDate    string  `json:"retestDate,omitempty"`
	RetestLow     float64 `json:"retestLow"`
	RetestConfirm bool    `json:"retestConfirm"` // 缩量确认 ★

	// 风控
	StopLoss float64 `json:"stopLoss"` // B段最低（铁律）
	Target   float64 `json:"target"`   // C≈A 等幅目标（展示）
	ChaseBan bool    `json:"chaseBan"` // 追高禁令
}

// maPeriod 趋势过滤用的均线窗口（S4：B 段收盘不破 20 日线）。
const maPeriod = 20

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

// WindowStart 返回该序列近 days 个交易日的窗口起点日期（过滤用）；
// days <= 0 或超出序列长度返回空串（不过滤）。
func WindowStart(bars []Candle, days int) string {
	if days <= 0 || days >= len(bars) {
		return ""
	}
	return bars[len(bars)-days].Date
}

// FindNPatterns 全市场扫描：返回截至序列末端仍存活的 N 字形态。
// panicDay 由调用方注入（指数单日跌幅 ≥1.5% 的交易日），nil 视为无恐慌日。
func FindNPatterns(symbol string, bars []Candle, p NPParams, panicDay func(string) bool) []NPSetup {
	var out []NPSetup
	if ValidNPParams(p) != nil || len(bars) == 0 {
		return out
	}
	if panicDay == nil {
		panicDay = func(string) bool { return false }
	}
	class := limitClassOf(symbol)
	ma := ma20Series(bars)
	first := max(p.CalmLookback+2, maPeriod)
	for aEnd := first; aEnd < len(bars); aEnd++ {
		for _, aLen := range []int{min(p.AMaxBars, 2), 1} { // 先试2根，再试1根
			aStart := aEnd - aLen + 1
			seg, ok := qualifyA(bars, aStart, aEnd, p, class)
			if !ok {
				continue
			}
			if s, alive := walkNP(symbol, bars, ma, aStart, aEnd, seg, p, panicDay); alive {
				out = append(out, s)
			}
			break // 该 aEnd 已按最长合格 A 段处理
		}
	}
	return out
}

// aSegment 合格 A 段的要素。
type aSegment struct {
	neckline, aStart, aVolAvg, baseVol float64
	hasLimitUp                          bool
}

// qualifyA 判定 [s,e] 是否构成 S1 合格的 A 段（含 S6 前期平静检查）。
func qualifyA(bars []Candle, s, e int, p NPParams, class limitClass) (aSegment, bool) {
	var seg aSegment
	if s < 1 {
		return seg, false
	}
	// 前期 5 日均量（A 段之前的 5 根）
	if s < 5 {
		return seg, false
	}
	var base float64
	for i := s - 5; i < s; i++ {
		base += bars[i].Volume
	}
	base /= 5
	if base <= 0 {
		return seg, false
	}
	// 每根都是放量阳线
	for i := s; i <= e; i++ {
		b := bars[i]
		if b.Close <= b.Open || b.Volume < base*p.AVolRatio {
			return seg, false
		}
		if isLimitUp(bars[i-1].Close, b.Close, class.pct(b.Date)) {
			seg.hasLimitUp = true
		}
	}
	seg.baseVol = base
	seg.aStart = bars[s-1].Close
	seg.neckline = bars[s].High
	for i := s; i <= e; i++ {
		seg.neckline = math.Max(seg.neckline, bars[i].High)
	}
	rise := (seg.neckline/seg.aStart - 1) * 100
	if rise < p.ARiseMinPct || rise > p.ARiseMaxPct {
		return seg, false
	}
	var volSum float64
	for i := s; i <= e; i++ {
		volSum += bars[i].Volume
	}
	seg.aVolAvg = volSum / float64(e-s+1)
	// S6：A 段前 CalmLookback 日累计涨幅 ≤ CalmMaxPct（高位二次冲高是诱多）
	calmFrom := s - 1 - p.CalmLookback
	if calmFrom < 0 {
		calmFrom = 0
	}
	if calm := (bars[s-1].Close/bars[calmFrom].Close - 1) * 100; calm > p.CalmMaxPct {
		return seg, false
	}
	if p.RequireLimitUpA && !seg.hasLimitUp {
		return seg, false
	}
	return seg, true
}

// walkNP 从 A 段 [s,e] 出发走 B 段→突破→回踩，返回存活形态。
func walkNP(symbol string, bars []Candle, ma []float64, s, e int, seg aSegment, p NPParams, panicDay func(string) bool) (NPSetup, bool) {
	last := len(bars) - 1
	amp := seg.neckline - seg.aStart // A段涨幅（价差）
	if amp <= 0 {
		return NPSetup{}, false
	}
	setup := NPSetup{
		Symbol: symbol, Stage: "b1", AsOf: bars[last].Date,
		AStartDate: bars[s].Date, AEndDate: bars[e].Date,
		Neckline: seg.neckline, AStart: seg.aStart,
		HasLimitUp: seg.hasLimitUp,
		ARisePct:   (seg.neckline/seg.aStart - 1) * 100,
		AVolRatio:  seg.aVolAvg / seg.baseVol,
		Retr382:    seg.neckline - amp*p.RetrMin,
		Retr50:     seg.neckline - amp*p.GoldenMax,
	}

	pullLow, bVolSum, bDays := math.MaxFloat64, 0.0, 0
	breakout := -1
	for d := e + 1; d <= e+p.BMaxDays && d <= last; d++ {
		day := bars[d]
		// 突破优先（B段需已满最小天数）
		if d-e >= p.BMinDays && day.Close >= seg.neckline*(1+p.BreakBufPct/100) {
			bAvg := bVolSum / float64(bDays)
			if day.Volume >= bAvg*p.CVolRatio && !panicDay(day.Date) {
				breakout = d
			} else {
				// 收盘过颈线但量不足（F-2 无量突破）或恐慌日：出局不作信号。
				return setup, false
			}
			break
		}
		// B 段逐日约束（S2/S3/S4）
		if ma[d] > 0 && day.Close < ma[d] {
			return setup, false // 收盘破 MA20
		}
		if day.Low <= seg.aStart {
			return setup, false // 破 A 段起点
		}
		if day.Close < day.Open && day.Volume > seg.aVolAvg*p.BVolRatio {
			return setup, false // 放量阴线（出货嫌疑）
		}
		pullLow = math.Min(pullLow, day.Low)
		if (seg.neckline-pullLow)/amp > p.RetrMax {
			return setup, false // 回撤超 0.618，M头风险
		}
		bVolSum += day.Volume
		bDays = d - e
		// 买点①：回撤进入黄金区 且 出现企稳信号
		if !setup.B1Triggered && (seg.neckline-pullLow)/amp >= p.RetrMin {
			if sig := detectStabilize(bars, d, ma, seg.aVolAvg); sig != "" && pullLow >= setup.Retr50 {
				setup.B1Triggered, setup.B1TriggerDate, setup.B1Signal = true, day.Date, sig
				setup.KeyDate = day.Date
			}
		}
	}
	if breakout < 0 {
		if last < e+p.BMaxDays && setup.B1Triggered {
			// 序列在 B 段窗口内结束：已触发企稳 → 留在 B1 观察名单
			return finalize(setup, bars, ma, e, pullLow, bVolSum, bDays, amp, seg, -1, p), true
		}
		return setup, false // 窗口耗尽未突破，或从未触发企稳
	}
	// 突破后的回踩窗口（B3 / F-3 假突破）
	for x := breakout + 1; x <= breakout+p.RetestDays && x <= last; x++ {
		day := bars[x]
		if day.Close < seg.neckline {
			return setup, false // 假突破：跌回颈线下方
		}
		if day.Low <= seg.neckline*(1+p.RetestBandPct/100) {
			setup.Stage = "b3"
			setup.RetestDate, setup.RetestLow = day.Date, day.Low
			bAvg := bVolSum / float64(bDays)
			if day.Volume <= bAvg*p.RetestVolRatio {
				setup.RetestConfirm = true
			}
		}
	}
	if last > breakout+p.RetestDays {
		return setup, false // 回踩窗口走完：转入持仓跟踪（本期不展示）
	}
	return finalize(setup, bars, ma, e, pullLow, bVolSum, bDays, amp, seg, breakout, p), true
}

// finalize 填充 B 段汇总与风控字段；breakout<0 表示仍在 B1 阶段。
func finalize(setup NPSetup, bars []Candle, ma []float64, e int, pullLow, bVolSum float64, bDays int, amp float64, seg aSegment, breakout int, p NPParams) NPSetup {
	last := len(bars) - 1
	end := last
	if breakout >= 0 {
		end = breakout - 1
		setup.Stage = "b2"
		if setup.RetestDate != "" {
			setup.Stage = "b3"
		}
		setup.BreakoutDate = bars[breakout].Date
		setup.BreakoutPrice = bars[breakout].Close
		bAvg := bVolSum / float64(bDays)
		setup.BreakoutVolRatio = bars[breakout].Volume / bAvg
		setup.DaysSinceBreakout = last - breakout
		setup.KeyDate = bars[breakout].Date
	}
	setup.BDays = bDays
	setup.BLow = pullLow
	setup.RetrRatio = (seg.neckline - pullLow) / amp
	if bDays > 0 {
		setup.BVolRatio = (bVolSum / float64(bDays)) / seg.aVolAvg
	}
	setup.MA20 = ma[end]
	setup.CurrentClose = bars[last].Close
	setup.StopLoss = pullLow
	setup.Target = pullLow + amp // C ≈ A 等幅测量
	// 追高禁令：C段涨幅超 A 段一半，或现价高出 5 日线 8%
	ma5 := bars[last].Close
	if last >= 4 {
		var sum float64
		for i := last - 4; i <= last; i++ {
			sum += bars[i].Close
		}
		ma5 = sum / 5
	}
	setup.ChaseBan = setup.CurrentClose-seg.neckline > amp*p.ChaseCRatio ||
		(ma5 > 0 && setup.CurrentClose > ma5*(1+p.ChaseMA5Pct/100))
	return setup
}

// detectStabilize 买点①的四种企稳信号（当日 d 命中任一返回信号名）。
func detectStabilize(bars []Candle, d int, ma []float64, aVolAvg float64) string {
	b, prev := bars[d], bars[d-1]
	body := math.Abs(b.Close - b.Open)
	shadow := math.Min(b.Open, b.Close) - b.Low
	// 1 缩量末端长下影：下影 ≥ 实体 2 倍、影线绝对幅度 ≥ 收盘价 0.8%（防
	// 小十字误报）且当日缩量
	if shadow > 0 && shadow >= 2*body && shadow >= b.Close*0.008 && b.Volume <= aVolAvg*0.8 {
		return "长下影"
	}
	// 2 阳包阴：阳线实体完整吞没前一根阴线实体
	if b.Close > b.Open && prev.Close < prev.Open && b.Open <= prev.Close && b.Close >= prev.Open {
		return "阳包阴"
	}
	// 3 连续两日地量后首根放量阳
	if d >= 2 && b.Close > b.Open && b.Volume > bars[d-1].Volume*1.3 &&
		bars[d-1].Volume <= bars[d-2].Volume*1.05 {
		var minVol float64 = math.MaxFloat64
		for i := max(d-22, 0); i <= d-3; i++ {
			minVol = math.Min(minVol, bars[i].Volume)
		}
		if minVol == math.MaxFloat64 || bars[d-1].Volume <= minVol*1.2 {
			return "地量后放量阳"
		}
	}
	// 4 跌至 20 日线后首次收阳（近两日内触及）
	touched := b.Low <= ma[d]*1.005 || prev.Low <= ma[d-1]*1.005
	if touched && b.Close > b.Open {
		return "20日线首阳"
	}
	return ""
}
