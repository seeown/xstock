package market

import (
	"testing"
	"time"
)

// screenBars 构造含 MA20 前置的标准形态：20 天平台（收 10、量 100）→
// 首板（收 11、高 11.05、量 300，量比 3）→ 2 天浅缩量回调（回撤 0.48，
// 未破半分位）→ 放量突破（收 11.2、量 216，量比 2）→ 回踩 2 天（低点进
// 前高带、收盘不破涨停价 11）→ 企稳阳线。首板=20，回调企稳=22，突破=23，
// 回踩企稳=25。
func screenBars(start string) []Candle {
	d0, _ := time.Parse("2006-01-02", start)
	bar := func(i int, o, h, l, c, v float64) Candle {
		return Candle{Date: d0.AddDate(0, 0, i).Format("2006-01-02"),
			Open: o, High: h, Low: l, Close: c, Volume: v}
	}
	bars := make([]Candle, 0, 26)
	for i := 0; i < 20; i++ {
		bars = append(bars, bar(i, 9.95, 10.1, 9.9, 10, 100))
	}
	bars = append(bars,
		bar(20, 10.5, 11.05, 10.4, 11, 300),
		bar(21, 10.85, 10.9, 10.58, 10.65, 50),
		bar(22, 10.62, 10.8, 10.55, 10.72, 40),
		bar(23, 10.8, 11.25, 10.7, 11.2, 216),
		bar(24, 11.15, 11.3, 10.9, 11.05, 100),
		bar(25, 11.0, 11.2, 10.95, 11.15, 90),
	)
	return bars
}

func TestScreenB3FullLifecycle(t *testing.T) {
	bars := screenBars("2024-01-02")
	setups := FindNSetups("600000.SH", bars, DefaultScreenParams())
	if len(setups) != 1 {
		t.Fatalf("want 1 alive setup, got %d", len(setups))
	}
	s := setups[0]
	if s.Stage != "b3" || s.KeyDate != bars[23].Date {
		t.Fatalf("stage/keyDate wrong: %+v", s)
	}
	if s.BoardDate != bars[20].Date || s.BreakoutDate != bars[23].Date {
		t.Fatalf("dates wrong: %+v", s)
	}
	if !s.B1Triggered || s.B1TriggerDate != bars[22].Date {
		t.Fatalf("B1 trigger wrong: %+v", s)
	}
	if !s.RetestTriggered || s.RetestTriggerDate != bars[25].Date {
		t.Fatalf("B3 trigger wrong: %+v", s)
	}
	if s.PullbackDays != 2 || s.PullbackDepth < 0.47 || s.PullbackDepth > 0.48 {
		t.Fatalf("pullback summary wrong: %+v", s)
	}
	if s.DaysSinceBreakout != 2 || s.RetestDays != 2 {
		t.Fatalf("post-breakout counters wrong: %+v", s)
	}
	if s.StopLossB3 < 10.93 || s.StopLossB3 > 10.95 { // 涨停价 11 × 0.995
		t.Fatalf("B3 stop wrong: %+v", s)
	}
}

func TestScreenB2AliveBeforeRetest(t *testing.T) {
	bars := screenBars("2024-01-02")[:24] // 序列在突破日（23）结束
	setups := FindNSetups("600000.SH", bars, DefaultScreenParams())
	if len(setups) != 1 || setups[0].Stage != "b2" {
		t.Fatalf("want stage b2, got %+v", setups)
	}
	if setups[0].RetestDays != 0 || setups[0].RetestTriggered {
		t.Fatalf("retest must not have started: %+v", setups[0])
	}
}

func TestScreenB1AliveDuringPullback(t *testing.T) {
	bars := screenBars("2024-01-02")[:23] // 序列在回调第 2 天（22）结束
	setups := FindNSetups("600000.SH", bars, DefaultScreenParams())
	if len(setups) != 1 || setups[0].Stage != "b1" {
		t.Fatalf("want stage b1, got %+v", setups)
	}
	s := setups[0]
	if s.KeyDate != bars[20].Date || !s.B1Triggered {
		t.Fatalf("B1 fields wrong: %+v", s)
	}
	if s.B1ZoneHigh < s.B1ZoneLow || s.B1ZoneLow <= s.RiseStart {
		t.Fatalf("B1 zone invalid: %+v", s)
	}
}

// 回踩期间任一日收盘跌破首板涨停价（11）→ setup 失效。
func TestScreenDeadRetestBreaksBoardClose(t *testing.T) {
	bars := screenBars("2024-01-02")
	bars[24] = Candle{Date: bars[24].Date, Open: 11.1, High: 11.2, Low: 10.85, Close: 10.9, Volume: 100}
	if got := FindNSetups("600000.SH", bars, DefaultScreenParams()); len(got) != 0 {
		t.Fatalf("回踩破涨停价必须失效, got %d", len(got))
	}
}

// 收盘越过前高但量比不达标（缩量突破）→ 突破失败，setup 结束。
func TestScreenDeadBreakoutWithoutVolume(t *testing.T) {
	bars := screenBars("2024-01-02")
	bars[23].Volume = 100 // 量比 ≈0.93 < 1.5
	if got := FindNSetups("600000.SH", bars, DefaultScreenParams()); len(got) != 0 {
		t.Fatalf("无量突破必须失效, got %d", len(got))
	}
}

// 回调窗口（5 天 + 次日）耗尽仍未突破 → 过期出列。
func TestScreenDeadPullbackTimeout(t *testing.T) {
	bars := screenBars("2024-01-02")[:23] // 0..22
	for i := 23; i <= 26; i++ {           // 平台横盘，始终未突破
		bars = append(bars, Candle{Date: bars[22].Date, Open: 10.7, High: 10.85, Low: 10.6, Close: 10.7, Volume: 50})
	}
	fixDates(bars)
	if got := FindNSetups("600000.SH", bars, DefaultScreenParams()); len(got) != 0 {
		t.Fatalf("回调超时必须失效, got %d", len(got))
	}
}

// 回调收盘跌破 MA20（下跌趋势反抽的首板）→ 失效。
func TestScreenDeadPullbackBreaksMA20(t *testing.T) {
	d0, _ := time.Parse("2006-01-02", "2024-01-02")
	bar := func(i int, o, h, l, c, v float64) Candle {
		return Candle{Date: d0.AddDate(0, 0, i).Format("2006-01-02"), Open: o, High: h, Low: l, Close: c, Volume: v}
	}
	bars := make([]Candle, 0, 22)
	for i := 0; i < 20; i++ { // 缓跌：12 → 10.1
		c := 12 - 0.1*float64(i)
		bars = append(bars, bar(i, c, c+0.05, c-0.05, c, 100))
	}
	bars = append(bars,
		bar(20, 10.5, 11.15, 10.4, 11.11, 300), // 首板 +10%，但 MA20≈11.01 仍压在上方
		bar(21, 11.0, 11.05, 10.85, 10.9, 50),  // 收盘 10.9 < MA20≈10.96 → 破线
	)
	if got := FindNSetups("600000.SH", bars, DefaultScreenParams()); len(got) != 0 {
		t.Fatalf("回调破 MA20 必须失效, got %d", len(got))
	}
}

func TestScreenDeadPullbackStructures(t *testing.T) {
	cases := map[string]func(bars []Candle){
		"破起涨点":   func(b []Candle) { b[21].Low = 9.95 },
		"放量回调":   func(b []Candle) { b[21].Volume = 250 },
		"回撤超半分位": func(b []Candle) { b[21].Low = 10.3 },
	}
	for name, mutate := range cases {
		bars := screenBars("2024-01-02")[:23]
		mutate(bars)
		if got := FindNSetups("600000.SH", bars, DefaultScreenParams()); len(got) != 0 {
			t.Fatalf("%s 必须失效, got %d", name, len(got))
		}
	}
}

func TestScreenUnsupportedSymbol(t *testing.T) {
	if got := FindNSetups("830799.BJ", screenBars("2024-01-02"), DefaultScreenParams()); len(got) != 0 {
		t.Fatalf("北交所不支持, got %d", len(got))
	}
}

func TestWindowStart(t *testing.T) {
	bars := screenBars("2024-01-02")
	if got := WindowStart(bars, 10); got != bars[16].Date {
		t.Fatalf("WindowStart(10) = %s, want %s", got, bars[16].Date)
	}
	if got := WindowStart(bars, 0); got != "" {
		t.Fatalf("WindowStart(0) must be empty, got %s", got)
	}
	if got := WindowStart(bars, 30); got != "" {
		t.Fatalf("超出序列长度应返回空, got %s", got)
	}
}

// fixDates 重排手工追加 K 线的日期（追加时复用了最后日期）。
func fixDates(bars []Candle) {
	d0, _ := time.Parse("2006-01-02", bars[0].Date)
	for i := range bars {
		bars[i].Date = d0.AddDate(0, 0, i).Format("2006-01-02")
	}
}
