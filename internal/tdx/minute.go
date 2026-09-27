package tdx

import (
	"fmt"
	"strings"
	"time"

	"github.com/bensema/gotdx/types"
)

// IndexMinutePoint is one slot of an index's intraday time-sharing chart.
type IndexMinutePoint struct {
	Time  string  `json:"time"`  // "09:30" … "15:00"，241 档时段坐标
	Price float64 `json:"price"` // 该分钟最新价
	Avg   float64 `json:"avg"`   // 当日均价线（服务端累计口径）
	Vol   int     `json:"vol"`   // 该分钟成交量
}

// IndexMinute is the latest session's minute series of one index.
type IndexMinute struct {
	Date     string             `json:"date"`     // 数据所属交易日（推断）
	PreClose float64            `json:"preClose"` // 昨收，分时基准线
	Points   []IndexMinutePoint `json:"points"`
}

// minuteSlots caps the series: 上午 09:30–11:30 共 121 档 + 下午 13:01–15:00 共 120 档。
const minuteSlots = 241

// FetchIndexMinute pulls the latest session's minute time-sharing data for an
// index together with the previous close (baseline). TDX always answers with
// the most recent session, so weekends replay Friday.
func (c *Client) FetchIndexMinute(def IndexDef) (*IndexMinute, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	reply, err := c.client.GetMinuteTimeData(def.Market, def.Code)
	if err != nil {
		if _, cerr := c.client.Connect(); cerr != nil {
			return nil, fmt.Errorf("通达信连接失败: %w", cerr)
		}
		reply, err = c.client.GetMinuteTimeData(def.Market, def.Code)
		if err != nil {
			return nil, fmt.Errorf("通达信分时拉取失败: %w", err)
		}
	}
	if len(reply.List) == 0 {
		return nil, fmt.Errorf("未获取到 %s 的分时数据", def.Name)
	}

	out := &IndexMinute{Points: make([]IndexMinutePoint, 0, len(reply.List))}
	// 昨收取自指数快照；快照失败时退化为分时首价（基准线退化为开盘）。
	if info, ierr := c.client.GetIndexInfo(def.Market, def.Code); ierr == nil && info != nil && info.PreClose > 0 {
		out.PreClose = info.PreClose
	}
	if out.PreClose == 0 {
		out.PreClose = reply.List[0].Price
	}
	out.Date = sessionDate(time.Now())

	n := len(reply.List)
	if n > minuteSlots {
		n = minuteSlots
	}
	for i := 0; i < n; i++ {
		p := reply.List[i]
		out.Points = append(out.Points, IndexMinutePoint{
			Time:  minuteSlotTime(i),
			Price: p.Price,
			Avg:   p.Avg,
			Vol:   p.Vol,
		})
	}
	return out, nil
}

// FetchMinute pulls the latest session's minute series for any SH/SZ symbol
// (stock or watchlist index) — TDX answers the same protocol call for both.
// The previous close comes from a one-symbol quote snapshot taken inside the
// same lock (FetchQuotes re-locks, so it must not be nested).
func (c *Client) FetchMinute(symbol string) (*IndexMinute, error) {
	if def, ok := IndexOf(strings.ToUpper(symbol)); ok {
		return c.FetchIndexMinute(def)
	}
	mkt, code, err := types.DetectMarket(strings.ToUpper(symbol))
	if err != nil || (mkt != types.MarketSH && mkt != types.MarketSZ && mkt != types.MarketBJ) {
		return nil, fmt.Errorf("仅支持沪深代码，例如 600519.SH")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	reply, err := c.client.GetMinuteTimeData(mkt.Uint8(), code)
	if err != nil {
		if _, cerr := c.client.Connect(); cerr != nil {
			return nil, fmt.Errorf("通达信连接失败: %w", cerr)
		}
		reply, err = c.client.GetMinuteTimeData(mkt.Uint8(), code)
		if err != nil {
			return nil, fmt.Errorf("通达信分时拉取失败: %w", err)
		}
	}
	if len(reply.List) == 0 {
		return nil, fmt.Errorf("未获取到 %s 的分时数据（可能停牌）", symbol)
	}

	out := &IndexMinute{Points: make([]IndexMinutePoint, 0, len(reply.List))}
	if qs, qerr := c.client.GetSecurityQuotes([]uint8{mkt.Uint8()}, []string{code}); qerr == nil && len(qs.List) > 0 && qs.List[0].PreClose > 0 {
		out.PreClose = qs.List[0].PreClose
	}
	if out.PreClose == 0 {
		out.PreClose = reply.List[0].Price
	}
	out.Date = sessionDate(time.Now())

	n := len(reply.List)
	if n > minuteSlots {
		n = minuteSlots
	}
	for i := 0; i < n; i++ {
		p := reply.List[i]
		out.Points = append(out.Points, IndexMinutePoint{
			Time:  minuteSlotTime(i),
			Price: p.Price,
			Avg:   p.Avg,
			Vol:   p.Vol,
		})
	}
	return out, nil
}

// minuteSlotTime maps slot index → wall-clock label. 0..120 是 09:30 起的
// 每分钟（121 档），121..240 是 13:01 起的每分钟（120 档），午休在轴上折叠。
func minuteSlotTime(i int) string {
	var t time.Time
	if i <= 120 {
		t = time.Date(0, 1, 1, 9, 30, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
	} else {
		t = time.Date(0, 1, 1, 13, 0, 0, 0, time.UTC).Add(time.Duration(i-120) * time.Minute)
	}
	return t.Format("15:04")
}

// sessionDate infers the trading day the minute series belongs to. TDX's
// minute reply carries no date and always serves the latest session: on a
// weekday at/after 09:15 that is today, before that it is the previous
// weekday; weekends roll back to Friday. 法定节假日无法本地判定，标签可能
// 偏早一天，仅用于展示。
func sessionDate(now time.Time) string {
	d := now
	switch d.Weekday() {
	case time.Saturday:
		d = d.AddDate(0, 0, -1)
	case time.Sunday:
		d = d.AddDate(0, 0, -2)
	default:
		if d.Hour() < 9 || (d.Hour() == 9 && d.Minute() < 15) {
			for {
				d = d.AddDate(0, 0, -1)
				if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
					break
				}
			}
		}
	}
	return d.Format("2006-01-02")
}
