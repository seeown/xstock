// Full A-share security listings from TDX, classified by board.
package tdx

import (
	"fmt"
	"sort"

	"github.com/bensema/gotdx/types"
)

// Listing is one stock row out of the exchange security lists.
type Listing struct {
	Symbol string // 600519.SH
	Code   string // 600519
	Name   string
	Market string // SH / SZ / BJ
	Board  string // 上证主板 / 科创板 / 深证主板 / 创业板 / 北交所
}

// BoardOf classifies a stock code into its board. Non-stock prefixes
// (indexes, funds, bonds, B-shares) return "".
func BoardOf(market, code string) string {
	switch market {
	case "SH":
		switch {
		case hasPrefix(code, "60"): // 600/601/603/605
			return "上证主板"
		case hasPrefix(code, "68"): // 688/689 科创板及CDR
			return "科创板"
		}
	case "SZ":
		switch {
		case hasPrefix(code, "30"): // 300/301/302
			return "创业板"
		case hasPrefix(code, "00"): // 000/001/002/003（中小板已并入主板）
			return "深证主板"
		}
	case "BJ":
		switch {
		case hasPrefix(code, "43"), hasPrefix(code, "83"), hasPrefix(code, "87"), hasPrefix(code, "92"):
			return "北交所"
		}
	}
	return ""
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// FetchAllStocks pages through the SH/SZ/BJ security lists and returns every
// stock (indexes, funds and bonds filtered out by code prefix).
func (c *Client) FetchAllStocks() ([]Listing, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.client.Connect(); err != nil {
		return nil, fmt.Errorf("通达信连接失败: %w", err)
	}

	markets := []struct {
		mkt  types.Market
		name string
	}{
		{types.MarketSZ, "SZ"},
		{types.MarketSH, "SH"},
		{types.MarketBJ, "BJ"},
	}
	var out []Listing
	seen := map[string]bool{}
	for _, m := range markets {
		for start := uint16(0); ; start += 1000 {
			reply, err := c.client.GetSecurityList(m.mkt.Uint8(), start)
			if err != nil {
				return nil, fmt.Errorf("拉取%s证券列表(start=%d)失败: %w", m.name, start, err)
			}
			for _, sec := range reply.List {
				code := sec.Code
				if len(code) != 6 || seen[code] {
					continue
				}
				board := BoardOf(m.name, code)
				if board == "" {
					continue // 指数、基金、债券、B股等
				}
				seen[code] = true
				out = append(out, Listing{
					Symbol: code + "." + m.name,
					Code:   code,
					Name:   sec.Name,
					Market: m.name,
					Board:  board,
				})
			}
			if reply.Count < 1000 {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	if len(out) == 0 {
		return nil, fmt.Errorf("证券列表为空")
	}
	return out, nil
}

// FetchConceptIndex downloads (or reuses the cached) concept block file and
// returns the bare-code → concept-names index for bulk imports.
func (c *Client) FetchConceptIndex() (map[string][]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.conceptsOf(""); err != nil {
		// The empty code lookup itself never fails once the file is parsed.
		return nil, err
	}
	index := make(map[string][]string, len(c.concepts.byCode))
	for code, names := range c.concepts.byCode {
		index[code] = names
	}
	return index, nil
}

// FetchIPODate returns the listing date of one stock from finance info.
// The connection is reused across calls; on a stale/dead connection it is
// re-established once and the request retried.
func (c *Client) FetchIPODate(symbol string) (string, error) {
	mkt, code, err := types.DetectMarket(symbol)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fin, err := c.client.GetFinanceInfo(mkt.Uint8(), code)
	if err != nil {
		if _, cerr := c.client.Connect(); cerr != nil {
			return "", fmt.Errorf("通达信连接失败: %w", cerr)
		}
		fin, err = c.client.GetFinanceInfo(mkt.Uint8(), code)
		if err != nil {
			return "", err
		}
	}
	if fin == nil || fin.IPODate == 0 {
		return "", fmt.Errorf("无上市日期")
	}
	d, err := parseCompactDate(int(fin.IPODate))
	if err != nil {
		return "", err
	}
	return d, nil
}
