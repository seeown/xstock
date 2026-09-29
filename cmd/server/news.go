package main

// 盘前资讯（一期）：东财全球快讯 + 新浪 7×24 双源合流 + 规则评分 +
// 自选池命中打标 + 隔夜行情带（美股四指数走腾讯、黄金/原油走新浪外盘）。
// 滚动更新走增量轮询：/api/news?since= 最后一条时间，只传新增。
// 来源方案（AKShare/Python 版）在此以 Go 原生 HTTP 客户端实现，不引入
// Python 依赖；公告（巨潮）与财经日历是二期。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"xstock/internal/store"
)

// NewsItem 统一 schema（来源方案的六道工序之「标准化」）。
type NewsItem struct {
	ID         string      `json:"id"`         // source:origID，前端合并去重键
	Time       time.Time   `json:"time"`       // 北京时间
	Source     string      `json:"source"`     // em / sina
	Title      string      `json:"title"`
	Summary    string      `json:"summary,omitempty"`
	URL        string      `json:"url,omitempty"`
	Hot        bool        `json:"hot"`       // 东财 titleColor 加红 / 关键词判定
	Categories []string    `json:"categories"` // policy / oversea / macro / ann / market
	Score      int         `json:"score"`
	Symbols    []NewsStock `json:"symbols,omitempty"`
}

// NewsStock 关联标的（正则+代码表抽取，方案一期口径：不上 NLP）。
type NewsStock struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	Watch  bool   `json:"watch"` // 自选池命中
}

// OvernightCell 隔夜行情带一格（美股指数/黄金/原油）。
type OvernightCell struct {
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	Flag    string  `json:"flag"` // US / CN / AU / OIL
	Price   float64 `json:"price"`
	ChgPct  float64 `json:"chgPct"`
	Time    string  `json:"time"`
}

// ---------- 评分与打标 ----------

var newsHotWords = []string{
	"重磅", "突发", "紧急", "重磅政策", "降准", "降息", "加息", "立案", "调查", "处罚",
	"业绩预增", "业绩预亏", "暴雷", "退市", "重组", "并购", "涨停", "跌停", "熔断",
	"国常会", "国务院", "中央", "央行", "证监会", "金融监管总局", "发改委", "财政部",
	"特朗普", "美联储", "鲍威尔", "非农", "CPI", "PCE", "FOMC", "关税", "制裁", "宣战",
}
var newsPolicyWords = []string{"国常会", "国务院", "中央", "政策", "央行", "证监会", "发改委", "财政部", "降准", "降息", "LPR", "MLF", "逆回购", "规", "意见", "方案", "通知"}
var newsOverseaWords = []string{"美股", "纳斯达克", "道琼斯", "标普", "美联储", "特朗普", "美国", "关税", "制裁", "欧盟", "日本", "中美", "中东", "俄", "原油", "黄金", "美元", "加息", "降息", "非农", "CPI", "FOMC"}
var newsAnnWords = []string{"公告", "业绩预", "业绩快报", "回购", "增持", "减持", "重组", "中标", "合同", "中标", "停牌", "复牌", "分 红", "分红", "股权激励", "问询"}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// scoreNews 规则评分（方案：来源权重 + 事件类型 + 自选命中 + 时效；LLM 后置）。
func scoreNews(item *NewsItem, watchSet map[string]bool, now time.Time) {
	score := 50
	if item.Hot {
		score += 25
	}
	text := item.Title + " " + item.Summary
	if containsAny(text, newsPolicyWords) {
		item.Categories = append(item.Categories, "policy")
		score += 15
	}
	if containsAny(text, newsOverseaWords) {
		item.Categories = append(item.Categories, "oversea")
		score += 8
	}
	if containsAny(text, newsAnnWords) {
		item.Categories = append(item.Categories, "ann")
		score += 8
	}
	if !containsAny(text, append(append([]string{}, newsPolicyWords...), newsAnnWords...)) && !containsAny(text, newsOverseaWords) {
		item.Categories = append(item.Categories, "market")
	}
	if len(item.Symbols) > 0 {
		score += 8
		for i := range item.Symbols {
			if watchSet[item.Symbols[i].Symbol] {
				item.Symbols[i].Watch = true
				score += 15
			}
		}
	}
	if now.Sub(item.Time) < time.Hour {
		score += 5
	}
	if score > 100 {
		score = 100
	}
	item.Score = score
}

// linkSymbols 正则+代码表抽取标的（名称精确匹配，长度≥2 防误命中）。
var newsSymbolRe = regexp.MustCompile(`[\x{4e00}-\x{9fa5}A-Za-z0-9*ST]{2,12}`)

func linkSymbols(text string, nameIdx map[string]store.Profile, watchSet map[string]bool) []NewsStock {
	seen := map[string]bool{}
	var out []NewsStock
	for _, m := range newsSymbolRe.FindAllString(text, -1) {
		if p, ok := nameIdx[m]; ok && !seen[p.Symbol] && len(m) >= 2 {
			// 名称须独立成词：前后贴着更多汉字时跳过（"中国船舶"不应命中"国船"类误配由代码表保证，此处防半截名）
			seen[p.Symbol] = true
			out = append(out, NewsStock{Symbol: p.Symbol, Name: p.Name, Watch: watchSet[p.Symbol]})
			if len(out) >= 4 {
				break
			}
		}
	}
	return out
}

// ---------- 源一：东方财富全球快讯 ----------

type emFastNewsResp struct {
	Data struct {
		FastNewsList []struct {
			Summary    string `json:"summary"`
			Code       string `json:"code"`
			TitleColor int    `json:"titleColor"`
			ShowTime   string `json:"showTime"`
			Title      string `json:"title"`
		} `json:"fastNewsList"`
	} `json:"data"`
}

func fetchEastmoneyNews(ctx context.Context, limit int) ([]NewsItem, error) {
	u := fmt.Sprintf("https://np-weblist.eastmoney.com/comm/web/getFastNewsList?client=web&biz=web_724&fastColumn=102&sortEnd=&pageSize=%d&req_trace=%d", limit, time.Now().UnixMilli())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r emFastNewsResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("东财快讯解析失败: %w", err)
	}
	out := make([]NewsItem, 0, len(r.Data.FastNewsList))
	for _, it := range r.Data.FastNewsList {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", it.ShowTime, time.Local)
		if err != nil {
			continue
		}
		title := it.Title
		if title == "" {
			title = it.Summary
		}
		out = append(out, NewsItem{
			ID: "em:" + it.Code, Time: t, Source: "em",
			Title: truncRunes(title, 80), Summary: truncRunes(it.Summary, 240),
			Hot: it.TitleColor != 0,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("东财快讯返回空")
	}
	return out, nil
}

// ---------- 源二：新浪 7×24 ----------

type sinaFeedResp struct {
	Result struct {
		Data struct {
			Feed struct {
				List []struct {
					CreateTime string `json:"create_time"`
					RichText   string `json:"rich_text"`
					ID         int64  `json:"id"`
				} `json:"list"`
			} `json:"feed"`
		} `json:"data"`
	} `json:"result"`
}

func fetchSinaNews(ctx context.Context, limit int) ([]NewsItem, error) {
	u := fmt.Sprintf("https://zhibo.sina.com.cn/api/zhibo/feed?page=1&page_size=%d&zhibo_id=152&tag_id=0&dire=f&dpc=1", limit)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r sinaFeedResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("新浪快讯解析失败: %w", err)
	}
	out := make([]NewsItem, 0, len(r.Result.Data.Feed.List))
	for _, it := range r.Result.Data.Feed.List {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", it.CreateTime, time.Local)
		if err != nil {
			continue
		}
		text := strings.ReplaceAll(it.RichText, "\r", "")
		// 新浪无独立标题字段：正文首行（常为【标题】或首句）作标题，全文作摘要
		lines := strings.SplitN(text, "\n", 2)
		title := strings.Trim(lines[0], " 【】")
		summary := ""
		if len(lines) > 1 {
			summary = strings.TrimSpace(lines[1])
		}
		if title == "" {
			title = truncRunes(text, 60)
		}
		out = append(out, NewsItem{
			ID: fmt.Sprintf("sina:%d", it.ID), Time: t, Source: "sina",
			Title: truncRunes(title, 80), Summary: truncRunes(summary, 240),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("新浪快讯返回空")
	}
	return out, nil
}

// ---------- 隔夜行情带 ----------

type overnightSpec struct {
	Key, Name, Flag, Code string
	Sina                  bool // sina hf_ 外盘 vs 腾讯 us 指数
}

var overnightSpecs = []overnightSpec{
	{Key: "dji", Name: "道琼斯", Flag: "US", Code: "usDJI"},
	{Key: "ndx", Name: "纳斯达克", Flag: "US", Code: "usIXIC"},
	{Key: "spx", Name: "标普 500", Flag: "US", Code: "usINX"},
	{Key: "hxc", Name: "金龙中国", Flag: "CN", Code: "usHXC"},
	{Key: "gold", Name: "纽约黄金", Flag: "AU", Code: "hf_GC", Sina: true},
	{Key: "oil", Name: "布伦特原油", Flag: "OIL", Code: "hf_OIL", Sina: true},
}

func fetchOvernight(ctx context.Context) ([]OvernightCell, error) {
	var tencentCodes, sinaCodes []overnightSpec
	for _, s := range overnightSpecs {
		if s.Sina {
			sinaCodes = append(sinaCodes, s)
		} else {
			tencentCodes = append(tencentCodes, s)
		}
	}
	out := make([]OvernightCell, 0, len(overnightSpecs))
	byKey := map[string]OvernightCell{}

	// 腾讯：美股指数（GBK；1=名 3=价 4=昨收 30=时间 31=涨跌 32=涨跌%）。
	// 逐格单码请求——批量逗号格式实测只回第一只，单码稳定。
	for _, s := range tencentCodes {
		u := "https://qt.gtimg.cn/q=" + s.Code
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		utf8, _ := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
		for _, line := range strings.Split(string(utf8), ";") {
			if !strings.Contains(line, "=\"") {
				continue
			}
			parts := strings.SplitN(line, "\"", 3)
			if len(parts) < 2 {
				continue
			}
			f := strings.Split(parts[1], "~")
			if len(f) < 33 {
				continue
			}
			price, chg := parseFloat(f[3]), parseFloat(f[32])
			byKey[s.Key] = OvernightCell{Key: s.Key, Name: f[1], Flag: s.Flag, Price: price, ChgPct: chg, Time: f[30]}
		}
	}
	// 新浪外盘：黄金/原油（GBK；0=价 7=昨结 6=时间 12=日期 13=名称）。同因批量问题逐格请求。
	for _, s := range sinaCodes {
		u := "https://hq.sinajs.cn/list=" + s.Code
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("Referer", "https://finance.sina.com.cn")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		utf8, _ := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
		for _, line := range strings.Split(string(utf8), ";") {
			if !strings.Contains(line, "=\"") {
				continue
			}
			parts := strings.SplitN(line, "\"", 3)
			if len(parts) < 2 {
				continue
			}
			f := strings.Split(parts[1], ",")
			if len(f) < 14 {
				continue
			}
			price, prev := parseFloat(f[0]), parseFloat(f[7])
			chg := 0.0
			if prev > 0 {
				chg = (price/prev - 1) * 100
			}
			// 逐格请求直接以 spec 为准，键用 s.Key；新浪名称在 f[13]（服务端给的中文名）
			name := f[13]
			if name == "" {
				name = s.Name
			}
			byKey[s.Key] = OvernightCell{Key: s.Key, Name: name, Flag: s.Flag, Price: price, ChgPct: chg, Time: f[12] + " " + f[6]}
		}
	}
	for _, s := range overnightSpecs {
		if c, ok := byKey[s.Key]; ok {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("隔夜行情全部源失败")
	}
	return out, nil
}

func flagOf(code string) string {
	for _, s := range overnightSpecs {
		if s.Code == code {
			return s.Flag
		}
	}
	return ""
}

func parseFloat(s string) float64 {
	var v float64
	fmt.Sscanf(strings.TrimSpace(s), "%g", &v)
	return v
}

func truncRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// ---------- 缓存与组装 ----------

type newsService struct {
	mu       sync.Mutex
	items    []NewsItem
	fetched  time.Time
	nameIdx  map[string]store.Profile
	namesAt  int
	client   http.Client
}

func newNewsService() *newsService {
	return &newsService{client: http.Client{Timeout: 10 * time.Second}, nameIdx: map[string]store.Profile{}}
}

func (ns *newsService) buildNameIdx(s *store.Store) {
	profiles := s.AllProfiles()
	if len(profiles) == ns.namesAt && ns.namesAt > 0 {
		return
	}
	idx := make(map[string]store.Profile, len(profiles))
	for _, p := range profiles {
		if len([]rune(p.Name)) >= 2 {
			idx[p.Name] = p
		}
	}
	ns.nameIdx = idx
	ns.namesAt = len(profiles)
}

// get 返回 60 秒缓存内的合流快讯；since 非零时只返回更新时间在其后的条目（增量轮询）。
func (ns *newsService) get(ctx context.Context, s *store.Store, since time.Time) ([]NewsItem, time.Time, error) {
	ns.mu.Lock()
	fresh := time.Since(ns.fetched) < 60*time.Second && len(ns.items) > 0
	if fresh {
		items, ts := ns.items, ns.fetched
		ns.mu.Unlock()
		return filterSince(items, since), ts, nil
	}
	ns.mu.Unlock()

	em, emErr := fetchEastmoneyNews(ctx, 50)
	sina, sinaErr := fetchSinaNews(ctx, 50)
	if emErr != nil && sinaErr != nil {
		// 双源皆败：退回旧缓存（哪怕过期）——降级链原则：简报不断供。
		ns.mu.Lock()
		items, ts := ns.items, ns.fetched
		ns.mu.Unlock()
		if len(items) > 0 {
			return filterSince(items, since), ts, nil
		}
		return nil, time.Time{}, fmt.Errorf("快讯双源失败: 东财=%v 新浪=%v", emErr, sinaErr)
	}
	merged := append(append([]NewsItem{}, em...), sina...)
	ns.buildNameIdx(s)
	watchSet, _ := s.WatchSet(ctx)
	now := time.Now()
	for i := range merged {
		merged[i].Symbols = linkSymbols(merged[i].Title+" "+merged[i].Summary, ns.nameIdx, watchSet)
		if !merged[i].Hot && containsAny(merged[i].Title, newsHotWords) {
			merged[i].Hot = true
		}
		scoreNews(&merged[i], watchSet, now)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Time.After(merged[j].Time) })
	if len(merged) > 200 {
		merged = merged[:200]
	}
	ns.mu.Lock()
	ns.items, ns.fetched = merged, now
	ns.mu.Unlock()
	return filterSince(merged, since), now, nil
}

func filterSince(items []NewsItem, since time.Time) []NewsItem {
	if since.IsZero() {
		return items
	}
	out := make([]NewsItem, 0, 32)
	for _, it := range items {
		if it.Time.After(since) {
			out = append(out, it)
		}
	}
	return out
}

// firstTradingNano 兜底解析 "2006-01-02T15:04:05.999999999Z07:00"。
var sinceLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"}

func parseSince(s string) time.Time {
	for _, l := range sinceLayouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// newsRoutes 挂 /api/news 与 /api/news/overnight。
func newsRoutes(mux *http.ServeMux, s *store.Store, ns *newsService) {
	mux.HandleFunc("GET /api/news", func(w http.ResponseWriter, r *http.Request) {
		since := parseSince(r.URL.Query().Get("since"))
		items, ts, err := ns.get(r.Context(), s, since)
		if err != nil {
			errorJSON(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "serverTime": ts.Format(time.RFC3339)})
	})
	mux.HandleFunc("GET /api/news/overnight", func(w http.ResponseWriter, r *http.Request) {
		v, err := liveFetch("news:overnight", 5*time.Minute, func() (any, error) {
			return fetchOvernight(r.Context())
		})
		if err != nil {
			errorJSON(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, v)
	})
}

var _ = url.QueryEscape // 保留 import 提示（如后续需要转义）
