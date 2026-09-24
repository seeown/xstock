package market

import (
	"fmt"
	"testing"
	"time"
)

// npBars 构造手册口径的标准形态：25 天平台（收 10 量 100）→
// A段 2 根放量阳线（收 10.6/11.4，量 250/250；涨幅14%，量=5日均量2.5×）
// → B段 4 天缩量回调（阴线量 60，低点 10.85 ≈ 回撤0.48）
// 其中第 4 天为阳包阴企稳 → 突破日（收 11.6 > 颈线×1.01，量 300 ≥ B均量2×）
// A段末=27，回调4天，突破=32。
func npBars(start string) []Candle {
	d0, _ := time.Parse("2006-01-02", start)
	bar := func(i int, o, h, l, c, v float64) Candle {
		return Candle{Date: d0.AddDate(0, 0, i).Format("2006-01-02"),
			Open: o, High: h, Low: l, Close: c, Volume: v}
	}
	bars := make([]Candle, 0, 33)
	for i := 0; i < 25; i++ {
		bars = append(bars, bar(i, 9.95, 10.1, 9.9, 10, 100))
	}
	bars = append(bars,
		bar(25, 10.05, 10.7, 10.0, 10.6, 250),  // A1 放量阳
		bar(26, 10.65, 11.4, 10.6, 11.4, 250),   // A2 放量阳，颈线 11.4，涨幅 14%
		bar(27, 11.3, 11.35, 11.05, 11.1, 60),   // B1 阴
		bar(28, 11.05, 11.1, 10.85, 10.95, 60),  // B2 阴，低点 10.85（回撤≈0.39）
		bar(29, 10.9, 11.1, 10.86, 11.06, 70),   // B3 阳包阴企稳（吞没 B2 实体）
		bar(30, 11.0, 11.15, 10.95, 11.1, 55),   // B4 缩量
		bar(31, 11.05, 11.1, 10.9, 11.0, 50),    // B5 缩量
		bar(32, 11.1, 11.65, 11.05, 11.6, 300),  // 突破：收 11.6 ≥ 11.4×1.01=11.51，量300 ≥ B均量×2
	)
	return bars
}

func noPanic(string) bool { return false }

func TestNPB1AliveDuringPullback(t *testing.T) {
	bars := npBars("2024-01-02")[:30] // 序列在企稳日(30)结束
	setups := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic)
	if len(setups) != 1 {
		t.Fatalf("want 1 alive b1 setup, got %d", len(setups))
	}
	s := setups[0]
	if s.Stage != "b1" || !s.B1Triggered {
		t.Fatalf("stage/trigger wrong: %+v", s)
	}
	if s.B1Signal != "阳包阴" {
		t.Fatalf("signal = %s, want 阳包阴", s.B1Signal)
	}
	if s.B1TriggerDate != bars[30-1].Date { // 序列截到 30 根，末根即企稳日
		t.Fatalf("trigger date = %s", s.B1TriggerDate)
	}
	if s.Neckline != 11.4 || s.Retr50 > s.BLow || s.BLow > s.Retr382 {
		t.Fatalf("levels wrong: neck=%.2f bLow=%.2f r50=%.2f r382=%.2f", s.Neckline, s.BLow, s.Retr50, s.Retr382)
	}
	if s.StopLoss != s.BLow {
		t.Fatalf("iron-rule stop must anchor B low")
	}
	if s.Target != s.BLow+(11.4-10.0) {
		t.Fatalf("target C≈A wrong: %.2f", s.Target)
	}
}

func TestNPB2Breakout(t *testing.T) {
	bars := npBars("2024-01-02")[:33] // 序列在突破日(32)结束
	setups := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic)
	if len(setups) != 1 || setups[0].Stage != "b2" {
		t.Fatalf("want b2, got %+v", setups)
	}
	s := setups[0]
	if s.BreakoutDate != bars[32].Date || s.BreakoutPrice != 11.6 {
		t.Fatalf("breakout wrong: %+v", s)
	}
	if s.BreakoutVolRatio < 5 { // 300 / B均量约 59
		t.Fatalf("breakout vol ratio = %.1f, want >5", s.BreakoutVolRatio)
	}
}

func TestNPB3Retest(t *testing.T) {
	// 突破后第2天回踩颈线（low 11.42 ≤ 11.4×1.02）缩量 → b3 确认
	bars := append(npBars("2024-01-02"),
		Candle{Date: "2024-02-10", Open: 11.55, High: 11.7, Low: 11.5, Close: 11.62, Volume: 120},
		Candle{Date: "2024-02-11", Open: 11.55, High: 11.6, Low: 11.42, Close: 11.55, Volume: 40}, // 回踩缩量
	)
	setups := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic)
	if len(setups) != 1 || setups[0].Stage != "b3" {
		t.Fatalf("want b3, got %+v", setups)
	}
	s := setups[0]
	if !s.RetestConfirm || s.RetestDate == "" {
		t.Fatalf("retest not confirmed: %+v", s)
	}
}

func TestNPFakeBreakoutKillsSetup(t *testing.T) {
	// 突破次日收盘跌回颈线下方 → 假突破，出局
	bars := append(npBars("2024-01-02"),
		Candle{Date: "2024-02-10", Open: 11.55, High: 11.7, Low: 11.2, Close: 11.3, Volume: 120},
	)
	setups := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic)
	if len(setups) != 0 {
		t.Fatalf("fake breakout must kill setup, got %d", len(setups))
	}
}

func TestNPPanicDayBlocksBreakout(t *testing.T) {
	bars := npBars("2024-01-02")[:33]
	panicAll := func(string) bool { return true }
	setups := FindNPatterns("600000.SH", bars, DefaultNPParams(), panicAll)
	if len(setups) != 0 {
		t.Fatalf("panic day breakout must be rejected, got %d", len(setups))
	}
}

func TestNPDeepRetraceKills(t *testing.T) {
	bars := npBars("2024-01-02")[:29]
	bars[28] = Candle{Date: bars[28].Date, Open: 11.0, High: 11.05, Low: 10.25, Close: 10.3, Volume: 60}
	// 回撤 (11.4-10.25)/1.4 = 0.82 > 0.618
	if got := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic); len(got) != 0 {
		t.Fatalf("deep retrace must kill, got %d", len(got))
	}
}

func TestNPVolumeDumpKills(t *testing.T) {
	bars := npBars("2024-01-02")[:28]
	bars[27] = Candle{Date: bars[27].Date, Open: 11.2, High: 11.25, Low: 11.0, Close: 11.05, Volume: 200} // 放量阴线
	if got := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic); len(got) != 0 {
		t.Fatalf("volume dump must kill, got %d", len(got))
	}
}

func TestNPHighBaseRejected(t *testing.T) {
	// A段前 20 日已涨 30% → 高位二次冲高（S6），否决
	bars := npBars("2024-01-02")
	for i := 0; i < 22; i++ { // 把平台改成从 7.7 涨到 10
		bars[i] = Candle{Date: bars[i].Date, Open: 7.6, High: 7.8, Low: 7.6, Close: 7.7 + float64(i)*0.105, Volume: 100}
	}
	if got := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic); len(got) != 0 {
		t.Fatalf("high-base A must be rejected, got %d", len(got))
	}
}

func TestNPSingleBarA(t *testing.T) {
	// 单根放量大阳（涨幅 12%）也构成 A 段
	d0, _ := time.Parse("2006-01-02", "2024-01-02")
	bar := func(i int, o, h, l, c, v float64) Candle {
		return Candle{Date: d0.AddDate(0, 0, i).Format("2006-01-02"), Open: o, High: h, Low: l, Close: c, Volume: v}
	}
	bars := make([]Candle, 0, 30)
	for i := 0; i < 25; i++ {
		bars = append(bars, bar(i, 9.95, 10.1, 9.9, 10, 100))
	}
	bars = append(bars,
		bar(25, 10.1, 11.25, 10.05, 11.2, 260), // 单根 +12%
		bar(26, 11.1, 11.15, 10.9, 10.95, 55),  // 回撤 0.32
		bar(27, 10.9, 11.0, 10.7, 10.75, 50),   // 回撤 0.46
		bar(28, 10.78, 11.05, 10.72, 10.95, 65), // 阳包阴
		bar(29, 10.95, 11.0, 10.8, 10.9, 55),
	)
	setups := FindNPatterns("600000.SH", bars, DefaultNPParams(), noPanic)
	if len(setups) != 1 {
		t.Fatalf("want single-bar-A setup, got %d", len(setups))
	}
	s := setups[0]
	if s.Stage != "b1" || !s.B1Triggered {
		t.Fatalf("stage wrong: %+v", s)
	}
	if fmt.Sprintf("%.2f", s.Neckline) != "11.25" {
		t.Fatalf("neckline = %.2f, want 11.25", s.Neckline)
	}
}

func TestDefaultNPParamsValid(t *testing.T) {
	if err := ValidNPParams(DefaultNPParams()); err != nil {
		t.Fatalf("default params invalid: %v", err)
	}
}
