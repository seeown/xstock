package market

import "testing"

// sentimentBars 三只票的小宇宙：
//   A(600001.SH) 平台→首板→二板（截至末日连板=2）
//   B(600002.SH) 平台→首板→炸板（末日高点触板、收盘未封）
//   C(600003.SH) 平台→平盘→跌停
func sentimentBars() map[string][]Candle {
	bar := func(d string, o, h, l, c float64) Candle {
		return Candle{Date: d, Open: o, High: h, Low: l, Close: c, Volume: 100}
	}
	return map[string][]Candle{
		"600001.SH": {
			bar("2024-01-02", 9.95, 10.1, 9.9, 10),
			bar("2024-01-03", 9.95, 10.1, 9.9, 10),
			bar("2024-01-04", 10.2, 11, 10.1, 11),       // 首板
			bar("2024-01-05", 11.2, 12.1, 11.1, 12.1),    // 二板
		},
		"600002.SH": {
			bar("2024-01-02", 9.95, 10.1, 9.9, 10),
			bar("2024-01-03", 9.95, 10.1, 9.9, 10),
			bar("2024-01-04", 10.2, 11, 10.1, 11),        // 首板
			bar("2024-01-05", 11.3, 12.1, 11.2, 11.5),    // 炸板：高点触 12.1 收 11.5
		},
		"000003.SZ": {
			bar("2024-01-02", 9.95, 10.1, 9.9, 10),
			bar("2024-01-03", 9.95, 10.1, 9.9, 10),
			bar("2024-01-04", 9.95, 10.1, 9.9, 10),
			bar("2024-01-05", 9.5, 9.6, 9, 9),            // 跌停
		},
	}
}

func sentimentSymbols() []string {
	return []string{"600001.SH", "600002.SH", "000003.SZ"}
}

func TestSentimentHistory(t *testing.T) {
	base := SentimentHistory(10, sentimentSymbols(), func(s string) []Candle { return sentimentBars()[s] })
	if len(base.Days) != 3 { // 首日无前收不可判定，可度量的是后 3 天
		t.Fatalf("want 3 days, got %d", len(base.Days))
	}
	last := base.Days[len(base.Days)-1]
	if last.Date != "2024-01-05" {
		t.Fatalf("last day = %s", last.Date)
	}
	if last.LimitUp != 1 || last.LimitDown != 1 || last.Broke != 1 {
		t.Fatalf("counts wrong: %+v", last)
	}
	if last.MaxBoards != 2 {
		t.Fatalf("maxBoards = %d, want 2 (A 的二板)", last.MaxBoards)
	}
	if last.YesterdayLimit != 2 {
		t.Fatalf("yesterdayLimit = %d, want 2", last.YesterdayLimit)
	}
	if last.Promoted != 1 {
		t.Fatalf("promoted = %d, want 1 (A 晋级二板)", last.Promoted)
	}
	if last.BreakRate < 49.9 || last.BreakRate > 50.1 {
		t.Fatalf("breakRate = %v, want 50", last.BreakRate)
	}
	if last.PromoteRate < 49.9 || last.PromoteRate > 50.1 {
		t.Fatalf("promoteRate = %v, want 50", last.PromoteRate)
	}
	if base.StreakAtEnd["600001.SH"] != 2 {
		t.Fatalf("A streak at end = %d, want 2", base.StreakAtEnd["600001.SH"])
	}
	if _, ok := base.StreakAtEnd["600002.SH"]; ok {
		t.Fatal("B 炸板日不应计入连板表")
	}
}

func TestComputeSentimentRealtimeIntraday(t *testing.T) {
	base := SentimentHistory(10, sentimentSymbols(), func(s string) []Candle { return sentimentBars()[s] })
	last := base.Days[len(base.Days)-1]
	pct10 := func(string) float64 { return 10 }
	quotes := []RTQuote{
		{Symbol: "600001.SH", Price: 13.31, PreClose: 12.1, High: 13.31}, // 今日再板 → 高度 3
		{Symbol: "600002.SH", Price: 11.8, PreClose: 11.5, High: 12.65},  // 触板未封 → 炸板
		{Symbol: "000003.SZ", Price: 8.1, PreClose: 9, High: 9.2},        // 跌停
	}
	rt := ComputeSentimentRealtime(base.AsOf, "10:00:00", quotes, base.StreakAtEnd, pct10, false, last.YesterdayLimit)
	if rt.LimitUp != 1 || rt.LimitDown != 1 || rt.Broke != 1 || rt.Touched != 2 {
		t.Fatalf("realtime counts wrong: %+v", rt)
	}
	if rt.MaxBoards != 3 {
		t.Fatalf("maxBoards = %d, want 3 (昨日二板 + 今日封板)", rt.MaxBoards)
	}
	if rt.Promoted != 1 || rt.PromoteRate < 49.9 || rt.PromoteRate > 50.1 {
		t.Fatalf("promotion wrong: %+v", rt)
	}
	if rt.BreakRate < 49.9 || rt.BreakRate > 50.1 {
		t.Fatalf("breakRate = %v, want 50", rt.BreakRate)
	}
}

func TestComputeSentimentRealtimeBarsIncludeToday(t *testing.T) {
	base := SentimentHistory(10, sentimentSymbols(), func(s string) []Candle { return sentimentBars()[s] })
	pct10 := func(string) float64 { return 10 }
	quotes := []RTQuote{
		{Symbol: "600001.SH", Price: 12.1, PreClose: 11, High: 12.1}, // 收盘后：日线已含今日二板
	}
	rt := ComputeSentimentRealtime(base.AsOf, "15:30:00", quotes, base.StreakAtEnd, pct10, true, 2)
	if rt.MaxBoards != 2 {
		t.Fatalf("maxBoards = %d, want 2 (连板数不再 +1)", rt.MaxBoards)
	}
	if rt.Promoted != 1 {
		t.Fatalf("promoted = %d, want 1 (streak>=2 视为晋级)", rt.Promoted)
	}
}
