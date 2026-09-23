package market

import (
	"math"
	"strings"
)

// 首板回调策略（N 字涨停）：首板涨停 → 缩量回调 → 温和放量突破首板高点。
// 与 FindNSignals 的通用 N 字策略相互独立，通过 BacktestFromSignals 共享
// 同一套交易模拟。
//
// 判定口径与已知限制：
//   - 涨停：收盘涨幅 ≥ 板块限额 − 0.2pp。日线为 QFQ 前复权，精确到分的
//     涨停价需要未复权序列；除权日交易所按除权基准价计算限额，前复权恰好
//     保留该比率，容差判定在复权序列上仍成立。
//   - 量比：标准口径，当日成交量 ÷ 前 5 个交易日平均成交量。经验区间
//     （板日 2.5~5、回调 0.4~0.8、突破 1.5~3）来自盘中量比口径；回调用
//     相对板日量的比例替代——标准量比的 5 日分母含首板日巨量，真实缩量
//     的回调也会被算成 1.4× 以上（2616 个真首板实测几乎全部误杀）。
//   - ST（5% 限额）依赖股票名称且历史状态不可追溯，首版不识别：ST 股
//     触及不到 10%/20% 涨停，天然被该策略排除。
//   - 北交所与无法识别的代码返回 0，不产生信号。

// limitTolerancePct 吸收涨停价按未复权前收盘四舍五入到分带来的误差
// （10% 板的真实当日收益率在 9.9%~10.1% 之间浮动）。
const limitTolerancePct = 0.2

// limitClass 把代码前缀解析成静态涨停类别，全市场扫描时按票解析一次，
// 避免逐根 K 线重复做字符串处理。
type limitClass struct {
	base    float64 // 常规涨停幅度；0 表示不支持
	chiNext bool    // 创业板：2020-08-24 前 10%，之后 20%
}

func limitClassOf(symbol string) limitClass {
	switch {
	case strings.HasSuffix(symbol, ".SH"):
		code := strings.TrimSuffix(symbol, ".SH")
		if strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689") {
			return limitClass{base: 20}
		}
		if strings.HasPrefix(code, "60") {
			return limitClass{base: 10}
		}
	case strings.HasSuffix(symbol, ".SZ"):
		code := strings.TrimSuffix(symbol, ".SZ")
		if strings.HasPrefix(code, "30") {
			return limitClass{base: 20, chiNext: true}
		}
		if strings.HasPrefix(code, "00") {
			return limitClass{base: 10}
		}
	case symbol == "DEMO":
		return limitClass{base: 10}
	}
	return limitClass{}
}

func (c limitClass) pct(date string) float64 {
	if c.base == 0 {
		return 0
	}
	if c.chiNext && date < "2020-08-24" {
		return 10
	}
	return c.base
}

// LimitUpPct 返回 symbol 在 date（YYYY-MM-DD）的涨停幅度百分数，0 表示
// 不支持或无法识别。创业板在 2020-08-24 注册制改革后由 10% 调整为 20%，
// 必须按日期区分，否则改革前的历史回测会把涨停错判成未涨停。
func LimitUpPct(symbol, date string) float64 {
	return limitClassOf(symbol).pct(date)
}

// isLimitUp 判断 close 相对 prevClose 是否达到涨停（pct 为百分数）。
func isLimitUp(prevClose, close, pct float64) bool {
	if prevClose <= 0 || pct <= 0 {
		return false
	}
	return close/prevClose*100-100 >= pct-limitTolerancePct
}

// oneWordBar 判断一字板：开高低收四价相等。QFQ 复权对同一根 K 线的四个
// 价格乘同一因子，等价关系保持。
func oneWordBar(b Candle) bool {
	return b.Open == b.High && b.High == b.Low && b.Low == b.Close
}

// volRatio 标准量比：当日成交量 ÷ 前 5 个交易日平均成交量。ok=false 表示
// 上市初期前置数据不足，调用方应放弃对该日的量比约束。
func volRatio(bars []Candle, i int) (ratio float64, ok bool) {
	if i < 5 {
		return 0, false
	}
	var sum float64
	for j := i - 5; j < i; j++ {
		sum += bars[j].Volume
	}
	if sum <= 0 {
		return 0, false
	}
	return bars[i].Volume / (sum / 5), true
}

// boardLimitUpBefore 检查 board 之前 lookback 个交易日内是否出现过涨停。
func boardLimitUpBefore(symbol string, bars []Candle, board, lookback int) bool {
	for i := max(board-lookback, 1); i < board; i++ {
		if isLimitUp(bars[i-1].Close, bars[i].Close, LimitUpPct(symbol, bars[i].Date)) {
			return true
		}
	}
	return false
}

// FirstBoardParams 首板回调策略参数，默认值对应量比经验区间：
// 板日 2.5~5（>8 天量易炸板）、回调期逐日 ≤0.8、突破日 1.5~3。
type FirstBoardParams struct {
	// BoardLookbackDays 首板判定回看窗口：窗口内再出现涨停则不算首板
	// （排除连板与近期已涨停），也用作突破参照的前高窗口。
	BoardLookbackDays int `json:"boardLookbackDays"`
	BoardVolRatioMin  float64 `json:"boardVolRatioMin"`
	BoardVolRatioMax  float64 `json:"boardVolRatioMax"`
	// ExcludeOneWordBoard 排除一字首板：无法成交且回调结构失真。
	ExcludeOneWordBoard bool `json:"excludeOneWordBoard"`

	PullbackMinDays int     `json:"pullbackMinDays"`
	PullbackMaxDays int     `json:"pullbackMaxDays"`
	PullbackMaxPct  float64 `json:"pullbackMaxPct"`
	// PullbackVolRatioMax 回调期单日成交量 ÷ 首板日成交量的上限：
	// 任一天超过即视为放量回调，直接否决。盘中量比口径的 0.8 折算到
	// 日线约为板日量的 0.7。
	PullbackVolRatioMax float64 `json:"pullbackVolRatioMax"`

	BreakoutBufferPct   float64 `json:"breakoutBufferPct"`
	BreakoutVolRatioMin float64 `json:"breakoutVolRatioMin"`
	// BreakoutVolRatioMax 突破日量比上限（温和放量），设大值可放宽。
	BreakoutVolRatioMax float64 `json:"breakoutVolRatioMax"`

	StopLossPct   float64 `json:"stopLossPct"`
	TakeProfitPct float64 `json:"takeProfitPct"`
	MaxHoldDays   int     `json:"maxHoldDays"`
}

func DefaultFirstBoardParams() FirstBoardParams {
	return FirstBoardParams{
		BoardLookbackDays:   10,
		BoardVolRatioMin:    2.5,
		BoardVolRatioMax:    8,
		ExcludeOneWordBoard: true,
		PullbackMinDays:     2,
		PullbackMaxDays:     5,
		PullbackMaxPct:      8,
		PullbackVolRatioMax: 0.7,
		BreakoutBufferPct:   0,
		BreakoutVolRatioMin: 1.5,
		BreakoutVolRatioMax: 3,
		StopLossPct:         5,
		TakeProfitPct:       15,
		MaxHoldDays:         20,
	}
}

func ValidFirstBoardParams(p FirstBoardParams) error {
	if p.BoardLookbackDays < 1 || p.BoardVolRatioMin <= 0 || p.BoardVolRatioMax <= p.BoardVolRatioMin ||
		p.PullbackMinDays < 1 || p.PullbackMaxDays < p.PullbackMinDays || p.PullbackMaxPct <= 0 ||
		p.PullbackVolRatioMax <= 0 || p.BreakoutVolRatioMin <= 0 || p.BreakoutVolRatioMax < p.BreakoutVolRatioMin ||
		p.StopLossPct <= 0 || p.TakeProfitPct <= 0 || p.MaxHoldDays < 1 {
		return errInvalidParams
	}
	return nil
}

func (p FirstBoardParams) exits() ExitParams {
	return ExitParams{StopLossPct: p.StopLossPct, TakeProfitPct: p.TakeProfitPct, MaxHoldDays: p.MaxHoldDays}
}

// FindFirstBoardSignals 识别「首板涨停 → 缩量回调 → 放量突破」结构。信号
// 在突破日收盘生成，回测按下一交易日开盘价入场：
//   - 首板日：收盘涨停、回看窗口内无涨停、量比在 [min,max]（天量首板易
//     烂板/炸板，直接否决）；
//   - 回调期：逐日成交量 ≤ 首板日量 × 上限（任一天放量回调即废弃该首
//     板）、低点不破首板起涨点（首板前收盘）、幅度受限；回调期内再涨停
//     视为连板结构；
//   - 突破日：收盘越过回看窗口与首板日的最高价，量比在 [min,max]。
func FindFirstBoardSignals(symbol string, bars []Candle, p FirstBoardParams) []Signal {
	var out []Signal
	if ValidFirstBoardParams(p) != nil {
		return out
	}
	// 量比需要 5 日前置均量，首板候选至少从 max(lookback,5) 开始。
	for board := max(p.BoardLookbackDays, 5); board+1 < len(bars); board++ {
		pct := LimitUpPct(symbol, bars[board].Date)
		if !isLimitUp(bars[board-1].Close, bars[board].Close, pct) {
			continue
		}
		if p.ExcludeOneWordBoard && oneWordBar(bars[board]) {
			continue
		}
		if boardLimitUpBefore(symbol, bars, board, p.BoardLookbackDays) {
			continue
		}
		boardVR, ok := volRatio(bars, board)
		if !ok || boardVR < p.BoardVolRatioMin || boardVR > p.BoardVolRatioMax {
			continue
		}
		boardHigh, startPrice := bars[board].High, bars[board-1].Close
		swingHigh := boardHigh
		for i := board - p.BoardLookbackDays; i < board; i++ {
			swingHigh = math.Max(swingHigh, bars[i].High)
		}
		requiredBreakout := swingHigh * (1 + p.BreakoutBufferPct/100)

		pullLow, pullMinVol, maxPullVR := math.MaxFloat64, math.MaxFloat64, 0.0
		for d := board + 1; d <= board+p.PullbackMaxDays && d < len(bars); d++ {
			day := bars[d]
			if isLimitUp(bars[d-1].Close, day.Close, LimitUpPct(symbol, day.Date)) {
				break // 回调期内再涨停：连板结构，不是首板回调
			}
			// 缩量约束用相对板日量的比例；标准量比的 5 日分母含板日巨量，
			// 会把真实缩量的回调误判成放量（见包注释）。
			if relVR := day.Volume / bars[board].Volume; relVR > p.PullbackVolRatioMax {
				break // 放量回调直接否决
			} else if relVR > maxPullVR {
				maxPullVR = relVR
			}
			pullLow = math.Min(pullLow, day.Low)
			pullMinVol = math.Min(pullMinVol, day.Volume)
			if pullLow <= startPrice || (boardHigh-pullLow)/boardHigh*100 > p.PullbackMaxPct {
				break // 破起涨点或回调过深
			}
			if d-board < p.PullbackMinDays || d+1 >= len(bars) {
				continue
			}
			brk := bars[d+1]
			if brk.Close < requiredBreakout {
				continue
			}
			brkVR, ok := volRatio(bars, d+1)
			if !ok || brkVR < p.BreakoutVolRatioMin || brkVR > p.BreakoutVolRatioMax {
				continue
			}
			dayChange := 0.0
			if prev := bars[d].Close; prev > 0 {
				dayChange = (brk.Close/prev - 1) * 100
			}
			out = append(out, Signal{
				Symbol: symbol, Date: brk.Date, Reason: "首板涨停-缩量回调-放量突破",
				BreakoutPrice: brk.Close, PriorHigh: swingHigh, PullbackLow: pullLow,
				RisePct: (boardHigh / startPrice - 1) * 100,
				PullbackPct:  (swingHigh - pullLow) / swingHigh * 100,
				VolumeRatio:  brkVR,
				DayChangePct: dayChange,
				BoardDate:      bars[board].Date,
				BoardVolRatio:  boardVR,
				PullbackDays:   d - board,
				PullbackVolRatio: maxPullVR,
				LimitPct:       pct,
				// StrongWash 洗盘金标准：回调期最小量缩到首板日量的 1/3 以下。
				StrongWash: pullMinVol <= bars[board].Volume/3,
			})
			break // 每个首板只取最近的一个突破信号
		}
	}
	return out
}
