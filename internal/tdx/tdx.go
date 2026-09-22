// Package tdx fetches daily bars from TDX quote servers via gotdx.
package tdx

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bensema/gotdx"
	"github.com/bensema/gotdx/proto"
	"github.com/bensema/gotdx/types"

	"nstock/internal/market"
)

// Client wraps a gotdx client. gotdx keeps its own host pool and reconnects
// on bad connections; the mutex keeps multi-request access serialized.
type Client struct {
	mu       sync.Mutex
	client   *gotdx.Client
	concepts *conceptIndex // concept block cache, see profile.go
}

func New() *Client { return NewRotated(0) }

// NewRotated builds a client whose preferred TDX host is rotated by index.
// When one host throttles or stalls, workers pinned to different hosts keep
// making progress instead of all hammering the same primary.
func NewRotated(index int) *Client {
	hosts := currentHosts()
	opts := make([]gotdx.Option, 0, len(hosts)+1)
	if len(hosts) > 0 {
		primary := hosts[index%len(hosts)]
		opts = append(opts, gotdx.WithTCPAddress(primary))
		pool := make([]string, 0, len(hosts)-1)
		for _, h := range hosts {
			if h != primary {
				pool = append(pool, h)
			}
		}
		if len(pool) > 0 {
			opts = append(opts, gotdx.WithTCPAddressPool(pool...))
		}
	}
	opts = append(opts, gotdx.WithTimeoutSec(8))
	return &Client{client: gotdx.New(opts...)}
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client.Disconnect()
}

// ErrClientWedged means a Connect() handshake hung past its watchdog; gotdx's
// handshake read has no deadline, so the caller must abandon this client
// (its mutex stays held by the stuck goroutine) and use a fresh one.
var ErrClientWedged = errors.New("tdx 握手挂起，客户端已报废，需更换连接")

// connectWatchdog runs Connect under a hard cap since gotdx's handshake read
// blocks forever against a server that accepts TCP but never answers.
func (c *Client) connectWatchdog() error {
	ch := make(chan error, 1)
	go func() {
		_, err := c.client.Connect()
		ch <- err
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(8 * time.Second):
		return ErrClientWedged
	}
}

// FetchRecentDaily fetches just the newest page of daily QFQ bars (up to 600,
// ≈2.5 years) in a single request — the daily updater's probe. The page itself
// carries any new trading days, so callers can append straight from it.
func (c *Client) FetchRecentDaily(symbol string) ([]market.Candle, error) {
	mkt, code, err := types.DetectMarket(symbol)
	if err != nil {
		return nil, err
	}
	if mkt != types.MarketSH && mkt != types.MarketSZ && mkt != types.MarketBJ {
		return nil, fmt.Errorf("仅支持沪深北代码，例如 600519.SH")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	page, err := c.client.GetKLine(types.KLINE_TYPE_DAILY, mkt.Uint8(), code, 0, klinePage, 1, types.AdjustQFQ)
	if err != nil {
		if cerr := c.connectWatchdog(); cerr != nil {
			return nil, cerr
		}
		page, err = c.client.GetKLine(types.KLINE_TYPE_DAILY, mkt.Uint8(), code, 0, klinePage, 1, types.AdjustQFQ)
		if err != nil {
			return nil, fmt.Errorf("通达信行情拉取失败: %w", err)
		}
	}
	return candlesFromBars(page.List), nil
}

// candlesFromBars filters placeholder/corrupt rows, dedupes by date and sorts
// ascending. Servers occasionally return byte-misaligned garbage (dates like
// 9013 or 199011, negative prices); the sanity bounds drop all of it.
func candlesFromBars(raw []proto.SecurityBar) []market.Candle {
	const minSaneDate, maxSaneDate = "1991-01-01", "2100-01-01"
	out := make([]market.Candle, 0, len(raw))
	seen := map[string]bool{}
	for _, b := range raw {
		if b.Close <= 0 || b.Open <= 0 {
			continue // placeholder rows before IPO
		}
		date := b.DateTime.Format("2006-01-02")
		if date < minSaneDate || date > maxSaneDate {
			continue // misaligned garbage
		}
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
	return out
}

// klinePage is the per-request page size for full-history downloads.
// gotdx's StockFullKLine pages by only 20 bars (150+ requests for a typical
// stock); 600 is what gotdx's own high-volume methods use and TDX accepts.
const klinePage = 600

// FetchDailyQFQ downloads the full daily history (forward-adjusted, 前复权)
// for a symbol like 600519.SH or 000001.SZ. The whole series is re-downloaded
// on purpose: QFQ prices are rebased whenever a new dividend occurs, so
// appending new dates to an old QFQ series would mix adjustment bases.
func (c *Client) FetchDailyQFQ(symbol string) ([]market.Candle, error) {
	mkt, code, err := types.DetectMarket(symbol)
	if err != nil {
		return nil, err
	}
	if mkt != types.MarketSH && mkt != types.MarketSZ && mkt != types.MarketBJ {
		return nil, fmt.Errorf("仅支持沪深北代码，例如 600519.SH")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	var raw []proto.SecurityBar
	for start := uint16(0); ; start += klinePage {
		page, err := c.client.GetKLine(types.KLINE_TYPE_DAILY, mkt.Uint8(), code, start, klinePage, 1, types.AdjustQFQ)
		if err != nil {
			// Cold or dropped connection: re-establish once and retry the page.
			if cerr := c.connectWatchdog(); cerr != nil {
				return nil, cerr
			}
			page, err = c.client.GetKLine(types.KLINE_TYPE_DAILY, mkt.Uint8(), code, start, klinePage, 1, types.AdjustQFQ)
			if err != nil {
				return nil, fmt.Errorf("通达信行情拉取失败: %w", err)
			}
		}
		// TDX answers Count=65535 (-1) with an empty list for symbols it has
		// no QFQ data for; an empty page or a short page ends pagination, and
		// the wrap guard keeps start from cycling forever past uint16 range.
		if len(page.List) == 0 {
			break
		}
		raw = append(raw, page.List...)
		if int(page.Count) < klinePage || start > 65535-klinePage {
			break
		}
	}

	out := candlesFromBars(raw)
	if len(out) == 0 {
		return nil, fmt.Errorf("未获取到 %s 的日K数据", symbol)
	}
	return out, nil
}
