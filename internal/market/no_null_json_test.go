package market

import (
	"bytes"
	"encoding/json"
	"testing"
)

// 回归围栏：Go 的 nil 切片会序列化成 JSON null，而前端类型把响应里的
// 列表字段全部约定为非空数组——2026-09-28 连板梯队首次出现"零晋级档"
// 时 promoted:null 直接把大盘页整页渲染崩掉。此测试用空数据构造各
// payload，任何列表字段冒出 null 都会在 CI 阶段被拦下。
func TestEmptyPayloadsMarshalNoNull(t *testing.T) {
	cases := map[string]any{
		// 服务端只经构造函数产出这些类型（零值 Ladder{} 不会上线路径），
		// 围栏对准真实构造路径的空数据产物。
		"ladder-empty":      BuildLadder("", "", nil, nil, func(string) float64 { return 10 }, false),
		"ladder-all-failed": BuildLadder("", "", nil, map[string]int{"600000.SH": 3}, nil, false),
		"backtest-no-trade": BacktestFromSignals("X", nil, nil, ExitParams{StopLossPct: 5, TakeProfitPct: 10, MaxHoldDays: 5}, 100000),
		"n-signals-empty":   FindNSignals("X", nil, DefaultParams()),
		"zt-signals-empty":  FindFirstBoardSignals("X", nil, DefaultFirstBoardParams()),
		"np-setups-empty":   FindNPatterns("X", nil, DefaultNPParams(), nil),
		"sentiment-history": SentimentHistory(30, nil, func(string) []Candle { return nil }),
	}
	for name, payload := range cases {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if i := bytes.Index(b, []byte(":null")); i >= 0 {
			t.Errorf("%s: payload 含 null 字段: ...%s...", name, clipAround(b, i))
		}
	}
}

func clipAround(b []byte, i int) []byte {
	lo, hi := i-30, i+20
	if lo < 0 {
		lo = 0
	}
	if hi > len(b) {
		hi = len(b)
	}
	return b[lo:hi]
}

// 精确涨跌停价判定的边界：低价股一分钱之差必须区分（2026-09-28 口径
// 事故的回归——昨收5.00跌停4.50，收4.51未封死，不得计入）。
func TestSealedLimitPriceBoundaries(t *testing.T) {
	cases := []struct {
		name           string
		prev, price    float64
		pct            float64
		wantUp, wantDn bool
	}{
		{"主板封死跌停", 5.00, 4.50, 10, false, true},
		{"主板差一分未封", 5.00, 4.51, 10, false, false},
		{"主板封死涨停", 5.00, 5.50, 10, true, false},
		{"主板涨停差一分", 5.00, 5.49, 10, false, false},
		{"创业20cm封死跌停", 10.00, 8.00, 20, false, true},
		{"创业20cm未到", 10.00, 8.01, 20, false, false},
		{"ST五厘米封死", 4.00, 3.80, 5, false, true},
		{"四舍五入到分-昨收9.99", 9.99, 8.99, 10, false, true}, // round(9.99*0.9,2)=8.99
	}
	for _, c := range cases {
		if got := isSealedLimitUp(c.prev, c.price, c.pct); got != c.wantUp {
			t.Errorf("%s: isSealedLimitUp=%v want %v", c.name, got, c.wantUp)
		}
		if got := isSealedLimitDown(c.prev, c.price, c.pct); got != c.wantDn {
			t.Errorf("%s: isSealedLimitDown=%v want %v", c.name, got, c.wantDn)
		}
	}
}

// 带宽守卫回归（2026-09-28 实际翻车案例）：档案名称带 ST 但实际已摘帽
// （恢复 10% 限制）的票，跌 5.4%~9.4% 被按 5% 误判跌停——守卫必须剔除。
func TestSealedBandGuard(t *testing.T) {
	// *ST岭南型：昨收 3.61，跌 9.41% 收 3.27；round(3.61*0.95,2)=3.43，
	// 3.27 <= 3.43 价格命中"5% 跌停"，但 -9.41% 远在 -5% 带外 → 剔除。
	if sealedDownGuarded(3.61, 3.27, 5) {
		t.Error("摘帽股按5%带外命中应被守卫剔除")
	}
	// 同价按真实 10% 限制：跌停价 round(3.61*0.9,2)=3.25，收 3.27 未到 → 不计。
	if sealedDownGuarded(3.61, 3.27, 10) {
		t.Error("10%限制下3.27未触及跌停价3.25")
	}
	// 真 ST 封死：昨收 4.00 跌停 3.80（-5%）→ 计入。
	if !sealedDownGuarded(4.00, 3.80, 5) {
		t.Error("真ST封死跌停应计入")
	}
	// 创业板 ST 跌 7%（20% 限制远未到）→ 调用方修复后 pct=20，天然不计。
	if sealedDownGuarded(10.00, 9.30, 20) {
		t.Error("20%限制下-7%不是跌停")
	}
}
