// Batched real-time quotes from TDX for the stock browser page.
package tdx

import (
	"fmt"
	"strings"

	"github.com/bensema/gotdx/types"
)

// Quote is the latest price snapshot of one symbol.
type Quote struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	PreClose  float64 `json:"preClose"`
	ChangePct float64 `json:"changePct"`
	Amount    float64 `json:"amount"` // 当日成交额（元）
	High      float64 `json:"high"`   // 当日最高价（含0=无数据）；炸板率等盘中判定用
	Open      float64 `json:"open"`   // 今开价（今日实时bar原料）
	Low       float64 `json:"low"`    // 当日最低价
	Vol       float64 `json:"vol"`    // 当日累计成交量（与日线同口径，实测为准）
	// 五档盘口一档：封板判定（封单维度）。涨停封单挂在买一，跌停封单挂在卖一。
	Bid1    float64 `json:"bid1"`
	BidVol1 int     `json:"bidVol1"`
	Ask1    float64 `json:"ask1"`
	AskVol1 int     `json:"askVol1"`
}

// quoteBatchCap stays below the TDX protocol limit (~80 codes per request).
const quoteBatchCap = 60

// FetchQuotes returns the latest quotes for up to 60 SH/SZ/BJ symbols in one
// protocol request. Symbols that cannot be resolved are silently omitted.
func (c *Client) FetchQuotes(symbols []string) ([]Quote, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("未指定代码")
	}
	if len(symbols) > quoteBatchCap {
		symbols = symbols[:quoteBatchCap]
	}
	var markets []uint8
	var codes []string
	symbolOf := map[string]string{} // "market|code" → symbol
	for _, sym := range symbols {
		mkt, code, err := types.DetectMarket(strings.ToUpper(sym))
		if err != nil {
			continue
		}
		markets = append(markets, mkt.Uint8())
		codes = append(codes, code)
		symbolOf[fmt.Sprintf("%d|%s", mkt.Uint8(), code)] = sym
	}
	if len(codes) == 0 {
		return nil, fmt.Errorf("没有可识别的代码")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	reply, err := c.client.GetSecurityQuotes(markets, codes)
	if err != nil {
		// A cold client has no connection yet; connect once and retry.
		if _, cerr := c.client.Connect(); cerr != nil {
			return nil, fmt.Errorf("通达信连接失败: %w", cerr)
		}
		reply, err = c.client.GetSecurityQuotes(markets, codes)
		if err != nil {
			return nil, fmt.Errorf("行情拉取失败: %w", err)
		}
	}

	out := make([]Quote, 0, len(reply.List))
	for _, item := range reply.List {
		sym, ok := symbolOf[fmt.Sprintf("%d|%s", item.Market, item.Code)]
		if !ok {
			continue
		}
		q := Quote{Symbol: sym, Price: item.Close, PreClose: item.PreClose, Amount: item.Amount, High: item.High,
			Open: item.Open, Low: item.Low, Vol: float64(item.Vol),
			Bid1: item.Bid1, BidVol1: item.BidVol1, Ask1: item.Ask1, AskVol1: item.AskVol1}
		// A zero close means no trade today (suspension); computing a change
		// against the previous close would show a bogus -100%.
		if q.Price > 0 && q.PreClose > 0 {
			q.ChangePct = (q.Price - q.PreClose) / q.PreClose * 100
		} else {
			q.Price, q.PreClose = 0, 0
		}
		out = append(out, q)
	}
	return out, nil
}
