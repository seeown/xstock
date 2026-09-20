// Package tdx fetches daily bars from TDX quote servers via gotdx.
package tdx

import (
	"fmt"
	"sort"
	"sync"

	"github.com/bensema/gotdx"
	"github.com/bensema/gotdx/proto"
	"github.com/bensema/gotdx/types"

	"nstock/internal/market"
)

// Client wraps a gotdx client. gotdx keeps its own host pool and reconnects
// on bad connections; the mutex keeps multi-request access serialized.
type Client struct {
	mu     sync.Mutex
	client *gotdx.Client
}

func New() *Client {
	hosts := gotdx.MainHostAddresses()
	opts := make([]gotdx.Option, 0, len(hosts)+1)
	if len(hosts) > 0 {
		opts = append(opts, gotdx.WithTCPAddress(hosts[0]))
	}
	if len(hosts) > 1 {
		opts = append(opts, gotdx.WithTCPAddressPool(hosts[1:]...))
	}
	opts = append(opts, gotdx.WithTimeoutSec(8))
	return &Client{client: gotdx.New(opts...)}
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client.Disconnect()
}

// FetchDailyQFQ downloads the full daily history (forward-adjusted, 前复权)
// for a symbol like 600519.SH or 000001.SZ. The whole series is re-downloaded
// on purpose: QFQ prices are rebased whenever a new dividend occurs, so
// appending new dates to an old QFQ series would mix adjustment bases.
func (c *Client) FetchDailyQFQ(symbol string) ([]market.Candle, error) {
	mkt, code, err := types.DetectMarket(symbol)
	if err != nil {
		return nil, err
	}
	if mkt != types.MarketSH && mkt != types.MarketSZ {
		return nil, fmt.Errorf("仅支持沪市(.SH)或深市(.SZ)代码，例如 600519.SH")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// StockFullKLine calls f on every bar; returning false keeps pagination
	// going until the earliest available bar is reached.
	bars, err := c.client.StockFullKLine(types.KLINE_TYPE_DAILY, mkt.Uint8(), code, 1, types.AdjustQFQ,
		func(proto.SecurityBar) bool { return false })
	if err != nil {
		return nil, fmt.Errorf("通达信行情拉取失败: %w", err)
	}

	out := make([]market.Candle, 0, len(bars))
	seen := map[string]bool{}
	for _, b := range bars {
		if b.Close <= 0 || b.Open <= 0 {
			continue // placeholder rows before IPO
		}
		date := b.DateTime.Format("2006-01-02")
		if seen[date] {
			continue
		}
		seen[date] = true
		out = append(out, market.Candle{
			Date:   date,
			Open:   b.Open,
			High:   b.High,
			Low:    b.Low,
			Close:  b.Close,
			Volume: b.Vol,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	if len(out) == 0 {
		return nil, fmt.Errorf("未获取到 %s 的日K数据", symbol)
	}
	return out, nil
}
