package tdx

import (
	"os"
	"testing"

	"github.com/bensema/gotdx/types"
)

// 手动诊断：TDX 服务器是否尊重 GetIndexBars 的 category 参数。
// 运行：go test ./internal/tdx -run TestProbeIndexCategories -v
// 需显式设置 XSTOCK_PROBE=1（避免常规 go test 打外网）。
func TestProbeIndexCategories(t *testing.T) {
	if os.Getenv("XSTOCK_PROBE") != "1" {
		t.Skip("set XSTOCK_PROBE=1 to run the manual TDX probe")
	}
	c := New()
	defer c.Close()
	for name, cat := range map[string]uint16{
		"daily":   types.KLINE_TYPE_DAILY,
		"weekly":  types.KLINE_TYPE_WEEKLY,
		"monthly": types.KLINE_TYPE_MONTHLY,
		"yearly":  types.KLINE_TYPE_YEARLY,
	} {
		reply, err := c.client.GetIndexBars(cat, IndexDefs[0].Market, IndexDefs[0].Code, 0, 8)
		if err != nil {
			t.Logf("%s: err %v", name, err)
			continue
		}
		t.Logf("%s: count=%d", name, len(reply.List))
		for _, b := range reply.List {
			t.Logf("  %s o=%.2f c=%.2f", b.DateTime.Format("2006-01-02"), b.Open, b.Close)
		}
	}
	// 大 count 诊断：验证分页页大小是否导致服务器返回日线数据。
	for _, cnt := range []uint16{80, 300, 600} {
		reply, err := c.client.GetIndexBars(types.KLINE_TYPE_WEEKLY, IndexDefs[0].Market, IndexDefs[0].Code, 0, cnt)
		if err != nil {
			t.Logf("weekly cnt=%d: err %v", cnt, err)
			continue
		}
		first, last := "-", "-"
		if len(reply.List) > 0 {
			first = reply.List[0].DateTime.Format("2006-01-02 15:04")
			last = reply.List[len(reply.List)-1].DateTime.Format("2006-01-02 15:04")
		}
		t.Logf("weekly cnt=%d -> reply.Count=%d listLen=%d first=%s last=%s", cnt, reply.Count, len(reply.List), first, last)
	}
}
