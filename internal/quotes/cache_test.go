package quotes

import (
	"testing"
	"time"
)

// 行情刷新窗口边界：工作日 09:10–15:10（含两端），周末全天关闭。
func TestInQuoteWindow(t *testing.T) {
	mk := func(wd time.Weekday, hm int) time.Time {
		// 2026-09-28 是周一；往下推到目标星期
		d := time.Date(2026, 9, 28, hm/100, hm%100, 0, 0, time.Local)
		for d.Weekday() != wd {
			d = d.AddDate(0, 0, 1)
		}
		return d
	}
	cases := []struct {
		name string
		wd   time.Weekday
		hm   int
		want bool
	}{
		{"周一开盘前1分钟", time.Monday, 909, false},
		{"周一窗口起点", time.Monday, 910, true},
		{"周一盘中", time.Monday, 1030, true},
		{"周一窗口终点", time.Monday, 1510, true},
		{"周一收盘后1分钟", time.Monday, 1511, false},
		{"周一深夜", time.Monday, 2330, false},
		{"周六盘中时段", time.Saturday, 1030, false},
		{"周日盘中时段", time.Sunday, 1030, false},
	}
	for _, c := range cases {
		if got := InQuoteWindow(mk(c.wd, c.hm)); got != c.want {
			t.Errorf("%s: InQuoteWindow=%v want %v", c.name, got, c.want)
		}
	}
}
