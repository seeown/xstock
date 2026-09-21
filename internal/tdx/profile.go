// Stock profile fetching: F10 text (name, industry, business), finance info
// (IPO date) and the TDX concept block file.
package tdx

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bensema/gotdx"
	"github.com/bensema/gotdx/types"
)

// conceptFile is the TDX block file holding 概念板块 memberships.
const conceptFile = "block_gn.dat"

// conceptTTL limits how often the concept block file is re-downloaded; it is
// a few hundred KB and changes at most daily.
const conceptTTL = time.Hour

// StockProfile is the raw metadata gathered from TDX for one symbol.
type StockProfile struct {
	Name     string
	Industry string
	Market   string // SH / SZ / BJ
	Board    string // 上证主板 / 科创板 / 深证主板 / 创业板 / 北交所
	ListDate string // YYYY-MM-DD, empty if unknown
	Business string
	Concepts []string
}

type conceptIndex struct {
	fetched time.Time
	// byCode maps a bare 6-digit code to the names of its concept blocks.
	byCode map[string][]string
}

var (
	// F10 tables use │-separated cells (│证券简称│贵州茅台│); some fields use
	// colons. Both shapes are matched, cell values cannot contain the separators.
	nameRe     = regexp.MustCompile(`证券简称\s*[:：│]\s*([^\s│|,，;；/]+)`)
	industryRe = regexp.MustCompile(`(?:通达信研究行业|所属行业|行业类别|所属板块)\s*[:：│]\s*([^\s│|,，;；/]+)`)

	businessMaxRunes = 6000
)

// FetchProfile assembles the profile of one SH/SZ stock from TDX: F10 text
// sections for name/industry/business, finance info for the IPO date, and the
// concept block file for concept tags. Missing pieces are left empty rather
// than failing the whole call.
func (c *Client) FetchProfile(symbol string) (*StockProfile, error) {
	mkt, code, err := types.DetectMarket(symbol)
	if err != nil {
		return nil, err
	}
	if mkt != types.MarketSH && mkt != types.MarketSZ && mkt != types.MarketBJ {
		return nil, fmt.Errorf("仅支持沪深北股票，例如 600519.SH")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// The low-level F10/block-file calls below go straight to exchange()
	// without gotdx's lazy-connect fallback, so make sure a live connection
	// exists first (reconnecting on every manual profile sync is fine).
	if _, err := c.client.Connect(); err != nil {
		return nil, fmt.Errorf("通达信连接失败: %w", err)
	}

	bundle, err := c.client.GetCompanyInfo(mkt.Uint8(), code)
	if err != nil {
		return nil, fmt.Errorf("通达信F10拉取失败: %w", err)
	}

	p := &StockProfile{Market: mkt.String(), Board: BoardOf(mkt.String(), code)}
	overview := sectionByName(bundle.Sections, "公司概况")
	p.Name = firstField(overview, nameRe)
	p.Industry = firstField(overview, industryRe)

	if bundle.Finance != nil && bundle.Finance.IPODate > 0 {
		if d, err := parseCompactDate(int(bundle.Finance.IPODate)); err == nil {
			p.ListDate = d
		}
	}

	var biz strings.Builder
	for _, name := range []string{"公司概况", "经营分析", "经营情况", "主营业务"} {
		if sec := sectionByName(bundle.Sections, name); sec != "" {
			if biz.Len() > 0 {
				biz.WriteString("\n\n")
			}
			biz.WriteString(sec)
		}
	}
	p.Business = truncateRunes(strings.TrimSpace(biz.String()), businessMaxRunes)

	concepts, err := c.conceptsOf(code)
	if err == nil {
		sort.Strings(concepts)
	} else {
		concepts = nil
	}
	p.Concepts = concepts
	return p, nil
}

// conceptsOf looks the bare code up in the cached concept block file.
func (c *Client) conceptsOf(code string) ([]string, error) {
	if c.concepts == nil || time.Since(c.concepts.fetched) > conceptTTL {
		groups, err := c.client.GetGroupedBlockFile(conceptFile)
		if err != nil {
			return nil, fmt.Errorf("下载概念板块失败: %w", err)
		}
		byCode := make(map[string][]string, 4096)
		for _, g := range groups {
			if g.BlockName == "" {
				continue
			}
			for _, member := range g.Codes {
				byCode[member] = append(byCode[member], g.BlockName)
			}
		}
		c.concepts = &conceptIndex{fetched: time.Now(), byCode: byCode}
	}
	return c.concepts.byCode[code], nil
}

// sectionByName returns the first section whose name contains the needle.
func sectionByName(sections []gotdx.CompanyInfoSection, needle string) string {
	for _, s := range sections {
		if strings.Contains(s.Name, needle) {
			return s.Content
		}
	}
	return ""
}

func firstField(text string, re *regexp.Regexp) string {
	if text == "" {
		return ""
	}
	if m := re.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// parseCompactDate turns TDX's YYYYMMDD integer into YYYY-MM-DD.
func parseCompactDate(v int) (string, error) {
	s := strconv.Itoa(v)
	if len(s) != 8 {
		return "", fmt.Errorf("unexpected date %d", v)
	}
	if _, err := time.Parse("20060102", s); err != nil {
		return "", err
	}
	return s[:4] + "-" + s[4:6] + "-" + s[6:], nil
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
