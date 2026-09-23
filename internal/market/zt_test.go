package market

import (
	"fmt"
	"testing"
	"time"
)

func TestLimitUpPct(t *testing.T) {
	cases := []struct {
		symbol, date string
		want         float64
	}{
		{"600519.SH", "2024-01-05", 10},
		{"000021.SZ", "2024-01-05", 10},
		{"300750.SZ", "2020-08-21", 10}, // 创业板注册制改革前
		{"300750.SZ", "2020-08-24", 20}, // 改革首日起 20%
		{"688981.SH", "2024-01-05", 20},
		{"830799.BJ", "2024-01-05", 0}, // 北交所暂不支持
		{"DEMO", "2024-01-05", 10},
		{"UNKNOWN", "2024-01-05", 0},
	}
	for _, c := range cases {
		if got := LimitUpPct(c.symbol, c.date); got != c.want {
			t.Fatalf("LimitUpPct(%s, %s) = %v, want %v", c.symbol, c.date, got, c.want)
		}
	}
}

func TestIsLimitUpTolerance(t *testing.T) {
	if !isLimitUp(10, 11, 10) {
		t.Fatal("10% 涨停必须命中")
	}
	if !isLimitUp(3.21, 3.53, 10) { // 分位取整后真实收益率 9.97%
		t.Fatal("分位取整误差内的涨停必须命中")
	}
	if isLimitUp(10, 10.95, 10) {
		t.Fatal("+9.5% 未涨停不应命中")
	}
}

func TestVolRatio(t *testing.T) {
	bars := make([]Candle, 7)
	for i := range bars {
		bars[i] = Candle{Volume: 100}
	}
	bars[6].Volume = 250
	if r, ok := volRatio(bars, 6); !ok || r != 2.5 {
		t.Fatalf("volRatio = %v (ok=%v), want 2.5", r, ok)
	}
	if _, ok := volRatio(bars, 4); ok {
		t.Fatal("前置不足 5 日时 ok 必须为 false")
	}
}

// ztBars 构造首板回调标准形态：20 天平台（收 10、量 100）→ 首板日
// （收 11、高 11.05、量 300，量比 3）→ 3 天缩量回调（量 50/50/40）→
// 突破日（收 11.2、量 216，量比 2）。首板在索引 20，突破在索引 24。
func ztBars(start string) []Candle {
	d0, _ := time.Parse("2006-01-02", start)
	bar := func(i int, open, high, low, close, vol float64) Candle {
		return Candle{Date: d0.AddDate(0, 0, i).Format("2006-01-02"),
			Open: open, High: high, Low: low, Close: close, Volume: vol}
	}
	bars := make([]Candle, 0, 25)
	for i := 0; i < 20; i++ {
		bars = append(bars, bar(i, 9.95, 10.1, 9.9, 10, 100))
	}
	bars = append(bars,
		bar(20, 10.5, 11.05, 10.4, 11, 300),   // 首板：+10%
		bar(21, 10.8, 10.9, 10.4, 10.6, 50),   // 回调 1：量比≈0.36
		bar(22, 10.6, 10.7, 10.45, 10.5, 50),  // 回调 2：量比≈0.38
		bar(23, 10.5, 10.65, 10.5, 10.55, 40), // 回调 3：量比≈0.33
		bar(24, 10.8, 11.25, 10.7, 11.2, 216), // 突破：量比 2.0
	)
	return bars
}

func TestFirstBoardSignalFound(t *testing.T) {
	bars := ztBars("2024-01-02")
	signals := FindFirstBoardSignals("600000.SH", bars, DefaultFirstBoardParams())
	if len(signals) != 1 {
		t.Fatalf("want 1 signal, got %d", len(signals))
	}
	s := signals[0]
	if s.Date != bars[24].Date || s.BoardDate != bars[20].Date {
		t.Fatalf("signal/broken dates: %+v", s)
	}
	if s.PullbackDays != 3 || s.LimitPct != 10 || !s.StrongWash {
		t.Fatalf("pullback fields wrong: %+v", s)
	}
	if s.VolumeRatio < 1.9 || s.VolumeRatio > 2.1 || s.BoardVolRatio < 2.9 || s.BoardVolRatio > 3.1 {
		t.Fatalf("volume ratios wrong: board=%v breakout=%v", s.BoardVolRatio, s.VolumeRatio)
	}
}

func TestFirstBoardBacktestTrade(t *testing.T) {
	bars := append(ztBars("2024-01-02"), Candle{Date: "2024-02-01",
		Open: 11.2, High: 13.0, Low: 11.1, Close: 12.9, Volume: 150})
	res := FirstBoardBacktest("600000.SH", bars, DefaultFirstBoardParams(), 100000)
	if len(res.Trades) != 1 {
		t.Fatalf("want 1 trade, got %d", len(res.Trades))
	}
	tr := res.Trades[0]
	if tr.BuyDate != "2024-02-01" || tr.ExitReason != "take_profit" {
		t.Fatalf("trade wrong: %+v", tr)
	}
}

// 回调期任何一天放量（单日量超过板日量 × 0.7）都必须否决整个首板。
func TestFirstBoardRejectsVolumePullback(t *testing.T) {
	bars := ztBars("2024-01-02")
	bars[22].Volume = 250 // 相对板日量 0.83 > 0.7
	if got := FindFirstBoardSignals("600000.SH", bars, DefaultFirstBoardParams()); len(got) != 0 {
		t.Fatalf("放量回调必须否决, got %d signals", len(got))
	}
}

// 天量首板（量比 > 上限）与缩量首板（量比 < 下限）都不产生信号。
func TestFirstBoardRejectsExtremeBoardVolume(t *testing.T) {
	for _, vol := range []float64{900, 150} { // 量比 9 与 1.5
		bars := ztBars("2024-01-02")
		bars[20].Volume = vol
		if got := FindFirstBoardSignals("600000.SH", bars, DefaultFirstBoardParams()); len(got) != 0 {
			t.Fatalf("首板量 %v 不应产生信号, got %d", vol, len(got))
		}
	}
}

// 回看窗口内已出现过涨停的不算首板（连板/近期涨停）。
func TestFirstBoardRejectsRecentBoard(t *testing.T) {
	bars := ztBars("2024-01-02")
	// 索引 15 再造一个 +10% 涨停（随后回到平台，无有效突破）。
	bars[15] = Candle{Date: bars[15].Date, Open: 10.2, High: 11.05, Low: 10.1, Close: 11, Volume: 300}
	if got := FindFirstBoardSignals("600000.SH", bars, DefaultFirstBoardParams()); len(got) != 0 {
		t.Fatalf("非首板必须被排除, got %d signals", len(got))
	}
}

func TestFirstBoardRejectsOneWordBoard(t *testing.T) {
	bars := ztBars("2024-01-02")
	bars[20] = Candle{Date: bars[20].Date, Open: 11, High: 11, Low: 11, Close: 11, Volume: 300}
	if got := FindFirstBoardSignals("600000.SH", bars, DefaultFirstBoardParams()); len(got) != 0 {
		t.Fatalf("一字首板默认排除, got %d signals", len(got))
	}
	p := DefaultFirstBoardParams()
	p.ExcludeOneWordBoard = false
	if got := FindFirstBoardSignals("600000.SH", bars, p); len(got) != 1 {
		t.Fatalf("关闭一字板过滤后应放行, got %d signals", len(got))
	}
}

// 创业板按日期套用 10%/20% 规则：改革前 +10% 是首板，改革后必须 +20%。
func TestFirstBoardChiNextDateRule(t *testing.T) {
	pre := FindFirstBoardSignals("300750.SZ", ztBars("2020-01-02"), DefaultFirstBoardParams())
	if len(pre) != 1 {
		t.Fatalf("2020 年创业板 10%% 规则下应产生信号, got %d", len(pre))
	}

	post := ztBars("2021-06-01")
	post[20] = Candle{Date: post[20].Date, Open: 11, High: 12.1, Low: 10.9, Close: 12, Volume: 300} // +20% 首板
	post[21] = Candle{Date: post[21].Date, Open: 11.6, High: 11.7, Low: 11.3, Close: 11.4, Volume: 50}
	post[22] = Candle{Date: post[22].Date, Open: 11.4, High: 11.5, Low: 11.25, Close: 11.3, Volume: 50}
	post[23] = Candle{Date: post[23].Date, Open: 11.3, High: 11.45, Low: 11.25, Close: 11.35, Volume: 40}
	post[24] = Candle{Date: post[24].Date, Open: 11.8, High: 12.3, Low: 11.7, Close: 12.2, Volume: 240} // 量比 2.0
	signals := FindFirstBoardSignals("300750.SZ", post, DefaultFirstBoardParams())
	if len(signals) != 1 || signals[0].LimitPct != 20 {
		t.Fatalf("注册制后创业板 20%% 首板应命中, got %+v", signals)
	}

	// 改革后 +10% 不再构成首板。
	only10 := ztBars("2021-06-01")
	if got := FindFirstBoardSignals("300750.SZ", only10, DefaultFirstBoardParams()); len(got) != 0 {
		t.Fatalf("改革后 +10%% 不算涨停, got %d signals", len(got))
	}
}

// 回测引擎解耦回归：Backtest 与 BacktestFromSignals + FindNSignals 等价。
func TestBacktestFromSignalsEquivalence(t *testing.T) {
	bars := DemoBars()
	a := Backtest("DEMO", bars, demoParams, 100000)
	b := BacktestFromSignals("DEMO", bars, FindNSignals("DEMO", bars, demoParams), demoParams.exits(), 100000)
	if fmt.Sprint(a.Trades) != fmt.Sprint(b.Trades) || fmt.Sprint(a.Metrics) != fmt.Sprint(b.Metrics) {
		t.Fatalf("refactored engine diverged:\n%v\n%v", a, b)
	}
}

func TestDefaultFirstBoardParamsValid(t *testing.T) {
	if err := ValidFirstBoardParams(DefaultFirstBoardParams()); err != nil {
		t.Fatalf("default first-board params must stay valid: %v", err)
	}
}
