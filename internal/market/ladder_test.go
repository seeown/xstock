package market

import "testing"

func TestBuildLadder(t *testing.T) {
	// 昨日梯队：A=2板，D=1板；快照：A 今日封板（晋级3板），
	// D 今日未封（失败），B 无昨日板但今日封板（新晋首板）。
	streaks := map[string]int{"600001.SH": 2, "600004.SH": 1}
	pct10 := func(string) float64 { return 10 }
	quotes := []RTQuote{
		{Symbol: "600001.SH", Price: 13.31, PreClose: 12.1, High: 13.31}, // +10% 封板
		{Symbol: "600004.SH", Price: 10.4, PreClose: 11, High: 11.2},     // 未封
		{Symbol: "600002.SH", Price: 11, PreClose: 10, High: 11},         // 新晋首板
		{Symbol: "600009.SH", Price: 0, PreClose: 10},                    // 停牌（无行情）
	}
	lad := BuildLadder("2024-01-05", "10:00:00", quotes, streaks, pct10, false)
	if lad.Final {
		t.Fatal("盘中口径 Final 应为 false")
	}
	if len(lad.Tiers) != 2 {
		t.Fatalf("want 2 tiers, got %d", len(lad.Tiers))
	}
	t2 := lad.Tiers[0]
	if t2.Height != 2 || t2.Total != 1 || len(t2.Promoted) != 1 || len(t2.Failed) != 0 {
		t.Fatalf("tier2 wrong: %+v", t2)
	}
	if t2.Promoted[0].Symbol != "600001.SH" || t2.Promoted[0].Height != 3 {
		t.Fatalf("promoted wrong: %+v", t2.Promoted[0])
	}
	if t2.PromoteRate != 100 {
		t.Fatalf("tier2 rate = %v", t2.PromoteRate)
	}
	t1 := lad.Tiers[1]
	if t1.Height != 1 || t1.Total != 1 || len(t1.Failed) != 1 || t1.PromoteRate != 0 {
		t.Fatalf("tier1 wrong: %+v", t1)
	}
	if len(lad.NewBoards) != 1 || lad.NewBoards[0].Symbol != "600002.SH" || lad.NewBoards[0].Height != 1 {
		t.Fatalf("newBoards wrong: %+v", lad.NewBoards)
	}
}

func TestBuildLadderSuspended(t *testing.T) {
	// 昨日有板但今日无行情（停牌）：计入失败、涨跌幅 0。
	streaks := map[string]int{"600005.SH": 3}
	quotes := []RTQuote{{Symbol: "600006.SH", Price: 11, PreClose: 10, High: 11}}
	lad := BuildLadder("2024-01-05", "10:00:00", quotes, streaks, func(string) float64 { return 10 }, false)
	if len(lad.Tiers) != 1 || lad.Tiers[0].Total != 1 || len(lad.Tiers[0].Failed) != 1 {
		t.Fatalf("suspended should land in failed: %+v", lad.Tiers)
	}
	if f := lad.Tiers[0].Failed[0]; f.Symbol != "600005.SH" || f.ChangePct != 0 {
		t.Fatalf("suspended fields wrong: %+v", f)
	}
	// 600006 封板且昨日无板 → 新首板
	if len(lad.NewBoards) != 1 || lad.NewBoards[0].Symbol != "600006.SH" {
		t.Fatalf("newBoards wrong: %+v", lad.NewBoards)
	}
}

func TestStreakAtPrev(t *testing.T) {
	// A: 首板(01-04)→二板(01-05)→非板(01-08)
	bars := []Candle{
		{Date: "2024-01-02", Close: 10},
		{Date: "2024-01-03", Close: 10},
		{Date: "2024-01-04", Close: 11},
		{Date: "2024-01-05", Close: 12.1},
		{Date: "2024-01-08", Close: 12.0},
	}
	base := SentimentHistory(10, []string{"600001.SH"}, func(string) []Candle { return bars })
	if base.StreakAtEnd["600001.SH"] != 0 {
		t.Fatalf("streak at end = %d, want 0 (末日非板)", base.StreakAtEnd["600001.SH"])
	}
	if base.StreakAtPrev["600001.SH"] != 2 {
		t.Fatalf("streak at prev = %d, want 2", base.StreakAtPrev["600001.SH"])
	}
}
