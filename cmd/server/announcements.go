package main

// 盘前资讯二期：巨潮公告（全量思维）。15:00 后扫近两日公告，编号
// （adjunctUrl PDF 路径）去重，标题关键词分类打标，巨潮官方来源满分
// 权重。公示在 /api/news 返回体的 anns 字段，与快讯同频更新。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Announcement 巨潮公告一条（标准化后）。
type Announcement struct {
	Symbol   string   `json:"symbol"`   // 600519.SH
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Time     string   `json:"time"`     // 北京时间 YYYY-MM-DD HH:MM
	Category string   `json:"category"` // forecast/restructure/buyback/holding/incentive/dividend/inquiry/other
	Score    int      `json:"score"`
	Watch    bool     `json:"watch"`
	URL      string   `json:"url"`      // PDF 链接
	Board    string   `json:"board,omitempty"`
}

// 分类关键词 → 分类码（按公告标题匹配，一个公告只归最重要的一类）。
var annCategories = []struct {
	Code    string
	Label   string
	Words   []string
	Weight  int
}{
	{"forecast", "业绩预告", []string{"业绩预告", "业绩预增", "业绩预亏", "业绩快报", "盈利预测", "净利润预", "年度报告", "季度报告", "半年度报告"}, 30},
	{"restructure", "重组", []string{"重组", "发行股份", "购买资产", "重大资产", "收购", "合并", "借壳", "吸收"}, 28},
	{"inquiry", "监管问询", []string{"问询", "关注函", "监管函", "立案", "调查", "处罚", "警示函", "纪律处分"}, 26},
	{"incentive", "股权激励", []string{"股权激励", "限制性股票", "股票期权", "员工持股"}, 20},
	{"buyback", "回购", []string{"回购", "注销"}, 18},
	{"holding", "增减持", []string{"增持", "减持", "协议转让", "大宗交易", "股东权益变动"}, 16},
	{"dividend", "分红", []string{"利润分配", "分红", "派息", "转增"}, 14},
	{"contract", "重大合同", []string{"中标", "合同", "订单", "战略合作", "框架协议"}, 16},
	{"other", "其他", nil, 8},
}

func classifyAnn(title string) (code, label string, weight int) {
	for _, c := range annCategories {
		for _, w := range c.Words {
			if strings.Contains(title, w) {
				return c.Code, c.Label, c.Weight
			}
		}
	}
	last := annCategories[len(annCategories)-1]
	return last.Code, last.Label, last.Weight
}

type cninfoResp struct {
	Total          int `json:"totalAnnouncement"`
	Announcements  []struct {
		SecCode            string `json:"secCode"`
		SecName            string `json:"secName"`
		AnnouncementTitle  string `json:"announcementTitle"`
		AnnouncementTime   int64  `json:"announcementTime"`
		AdjunctURL         string `json:"adjunctUrl"`
		ColumnID           string `json:"columnId"`
	} `json:"announcements"`
}

// fetchCninfoAnn 拉一个市场（column=szse 或 sse）近 N 日公告，按 pageSize 分页。
func fetchCninfoAnn(ctx context.Context, column string, days int, pageSize int) ([]Announcement, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -days)
	seDate := start.Format("2006-01-02") + "~" + end.Format("2006-01-02")
	var out []Announcement
	seen := map[string]bool{}
	client := &http.Client{Timeout: 15 * time.Second}
	for page := 1; page <= 5; page++ { // 5 页 × 30 条 = 150 条/市场，盘前够用
		form := url.Values{
			"pageNum":  {fmt.Sprint(page)},
			"pageSize": {fmt.Sprint(pageSize)},
			"column":   {column},
			"tabName":  {"fulltext"},
			"seDate":   {seDate},
			"isHLtitle": {"true"},
			"sortName": {"time"},
			"sortType": {"desc"},
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			"http://www.cninfo.com.cn/new/hisAnnouncement/query", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var r cninfoResp
		if err := json.Unmarshal(body, &r); err != nil {
			return out, fmt.Errorf("巨潮解析失败: %w", err)
		}
		for _, a := range r.Announcements {
			if seen[a.AdjunctURL] || a.AdjunctURL == "" {
				continue // 编号去重（同 PDF 可能跨页出现）
			}
			seen[a.AdjunctURL] = true
			t := time.Unix(a.AnnouncementTime/1000, 0)
			// 巨潮标题自带 <em> 高亮标签，剥掉
			title := strings.NewReplacer("<em>", "", "</em>", "").Replace(a.AnnouncementTitle)
			out = append(out, Announcement{
				Symbol: a.SecCode + suffixOf(a.SecCode),
				Name:   a.SecName,
				Title:  title,
				Time:   t.Format("2006-01-02 15:04"),
				URL:    "http://static.cninfo.com.cn/" + a.AdjunctURL,
			})
		}
		if len(r.Announcements) < pageSize {
			break // 拉完了
		}
	}
	return out, nil
}

// suffixOf 代码前缀推交易所后缀（比按 column 参数可靠：巨潮两市场查询结果有交叉）。
func suffixOf(code string) string {
	switch {
	case strings.HasPrefix(code, "60"), strings.HasPrefix(code, "68"), strings.HasPrefix(code, "9"):
		return ".SH"
	case strings.HasPrefix(code, "0"), strings.HasPrefix(code, "3"):
		return ".SZ"
	case strings.HasPrefix(code, "8"), strings.HasPrefix(code, "4"):
		return ".BJ"
	}
	return ""
}

// boardOf 代码推算板块标签。
func boardOf(symbol string) string {
	code := strings.TrimSuffix(strings.TrimSuffix(symbol, ".SZ"), ".SH")
	switch {
	case strings.HasPrefix(code, "30"):
		return "创业板"
	case strings.HasPrefix(code, "68"):
		return "科创板"
	case strings.HasPrefix(code, "60"):
		return "沪主板"
	case strings.HasPrefix(code, "00"):
		return "深主板"
	case strings.HasPrefix(code, "8"), strings.HasPrefix(code, "4"):
		return "北交所"
	}
	return ""
}

// fetchAnnouncements 双市场合并 + 跨市场按 PDF 编号去重 + 分类打标 +
// 单股限 2 条（重组一揽子常含券商/律师/评估多份配套文件，刷屏无信息量）+ 评分排序。
func fetchAnnouncements(ctx context.Context, watchSet map[string]bool) ([]Announcement, error) {
	sz, szErr := fetchCninfoAnn(ctx, "szse", 2, 30)
	sh, shErr := fetchCninfoAnn(ctx, "sse", 2, 30)
	if szErr != nil && shErr != nil {
		return nil, fmt.Errorf("巨潮双市场失败: 沪=%v 深=%v", shErr, szErr)
	}
	seenURL := map[string]bool{}
	anns := make([]Announcement, 0, len(sz)+len(sh))
	for _, a := range append(sz, sh...) {
		if seenURL[a.URL] {
			continue // 两市场查询结果交叉：同 PDF 只留一份
		}
		seenURL[a.URL] = true
		anns = append(anns, a)
	}
	for i := range anns {
		code, _, weight := classifyAnn(anns[i].Title)
		anns[i].Category = code
		score := 40 + weight // 巨潮官方来源基础分 40
		if watchSet[anns[i].Symbol] {
			anns[i].Watch = true
			score += 25
		}
		if score > 100 {
			score = 100
		}
		anns[i].Score = score
		anns[i].Board = boardOf(anns[i].Symbol)
	}
	// 排序：评分降序 → 时间降序
	sort.Slice(anns, func(i, j int) bool {
		if anns[i].Score != anns[j].Score {
			return anns[i].Score > anns[j].Score
		}
		return anns[i].Time > anns[j].Time
	})
	out := make([]Announcement, 0, 100)
	perSym := map[string]int{}
	for _, a := range anns {
		if perSym[a.Symbol] >= 2 {
			continue
		}
		perSym[a.Symbol]++
		out = append(out, a)
		if len(out) >= 100 {
			break
		}
	}
	return out, nil
}

// fetchBoards 简化：从 store profiles 取板块映射，由调用方传入。
// fetchCalendar 今日/近日关键日程（内置规则 + 已知 FOMC 日期）。
type CalEvent struct {
	Date string `json:"date"` // YYYY-MM-DD
	Time string `json:"time"` // HH:MM 或空
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	Hot  bool   `json:"hot,omitempty"`
}

func fetchCalendar(now time.Time) []CalEvent {
	var out []CalEvent
	today := now.Format("2006-01-02")
	day := now.Day()
	weekday := now.Weekday()

	// 规则推算：LPR 每月 20 日 09:00（遇假顺延，此处取规则值）
	if day == 20 || day == 19 {
		note := "今日公布"
		if day == 19 {
			note = "明日公布"
		}
		out = append(out, CalEvent{Date: today, Time: "09:00", Name: "LPR 报价（每月20日）", Hot: true, Note: note})
	}
	// PMI 终值每月 1 日、财新 PMI 每月初
	if day <= 2 {
		out = append(out, CalEvent{Date: today, Time: "09:45", Name: "财新制造业 PMI", Hot: true})
	}
	// 月末：制造业 PMI 官方（每月最后一个日历日次日=1日，取30/31日提示）
	if day >= 29 {
		out = append(out, CalEvent{Date: today, Time: "09:30", Name: "官方制造业 PMI（次日晨）", Note: "次月1日公布"})
	}
	// CPI/PPI 每 9 日前后
	if day >= 8 && day <= 10 {
		out = append(out, CalEvent{Date: today, Time: "09:30", Name: "CPI / PPI（月中）", Hot: true})
	}
	// 美国非农：每月第一个周五 20:30（冬令时 21:30）
	if weekday == time.Friday && day <= 7 {
		out = append(out, CalEvent{Date: today, Time: "20:30", Name: "美国非农就业（今晚）", Hot: true})
	}
	// 集合竞价节奏（收盘/落库节奏由前端静态卡展示，此处不重复）
	out = append(out,
		CalEvent{Date: today, Time: "09:15", Name: "集合竞价开始", Note: "竞价异动页同步监测"},
	)
	return out
}
