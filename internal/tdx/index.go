package tdx

import (
	"fmt"
	"sort"

	"github.com/bensema/gotdx/proto"
	"github.com/bensema/gotdx/types"

	"nstock/internal/market"
)

// IndexDef describes one market index that the dashboard can display.
type IndexDef struct {
	Symbol string // storage/API symbol, e.g. 000001.SH
	Code   string // TDX protocol code, e.g. 000001
	Market uint8  // TDX market id
	Name   string // Chinese display name
}

// IndexDefs is the curated index watchlist used by the market page.
var IndexDefs = []IndexDef{
	{Symbol: "000001.SH", Code: "000001", Market: types.MarketSH.Uint8(), Name: "上证指数"},
	{Symbol: "399001.SZ", Code: "399001", Market: types.MarketSZ.Uint8(), Name: "深证成指"},
	{Symbol: "399006.SZ", Code: "399006", Market: types.MarketSZ.Uint8(), Name: "创业板指"},
	{Symbol: "000688.SH", Code: "000688", Market: types.MarketSH.Uint8(), Name: "科创50"},
}

// IndexOf returns the watchlist definition for a storage symbol.
func IndexOf(symbol string) (IndexDef, bool) {
	for _, def := range IndexDefs {
		if def.Symbol == symbol {
			return def, true
		}
	}
	return IndexDef{}, false
}

// FetchDailyIndex downloads the full unadjusted daily history for a market
// index. Indices have no ex-dividend events, so no QFQ adjustment is needed.
func (c *Client) FetchDailyIndex(def IndexDef) ([]market.Candle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var all []proto.IndexBar
	page := uint16(20)
	for start := uint16(0); ; start += page {
		reply, err := c.client.GetIndexBars(types.KLINE_TYPE_DAILY, def.Market, def.Code, start, page)
		if err != nil {
			return nil, fmt.Errorf("通达信指数K线拉取失败: %w", err)
		}
		all = append(reply.List, all...)
		if reply.Count < page {
			break
		}
	}

	out := make([]market.Candle, 0, len(all))
	seen := map[string]bool{}
	for _, b := range all {
		if b.Close <= 0 || b.Open <= 0 {
			continue
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
		return nil, fmt.Errorf("未获取到 %s 的日K数据", def.Name)
	}
	return out, nil
}
