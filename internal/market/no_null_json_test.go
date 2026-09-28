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
