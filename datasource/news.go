package datasource

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// NewsItem 一条板块/标的相关新闻摘要。
type NewsItem struct {
	Date    string `json:"date"`
	Title   string `json:"title"`
	Source  string `json:"source"`
	Content string `json:"content"` // 已剥 <em>...</em> 高亮标签
	URL     string `json:"url"`
}

// NewsFetcher 通过 EastMoney / Sina / 新浪财经 等开放接口抓取真实新闻列表。
//
// 设计参考 ValueCell 的 news_agent/tools.py：
//   - 把"工具调用"前置成本进程内的 HTTP 抓取（避免依赖外部 LLM 的 web_search 能力）；
//   - 多关键词扇出（板块名 + 板块龙头 + 核心概念）→ 合并去重；
//   - 按时间倒序排序，截取最近 limit 条作为 LLM 输入；
//   - LLM 仅做摘要，不再凭空生成新闻。
type NewsFetcher struct {
	HTTP *http.Client
}

func NewNewsFetcher() *NewsFetcher {
	return &NewsFetcher{HTTP: &http.Client{Timeout: 8 * time.Second}}
}

// FetchSectorNews 单关键词抓取（兼容旧调用）。
func (f *NewsFetcher) FetchSectorNews(keyword string, limit int) []NewsItem {
	return f.FetchMulti([]string{keyword}, limit)
}

// FetchMulti 多关键词扇出抓取，去重 + 按时间倒序，返回最近 limit 条。
//
// 多源策略：
//  1. EastMoney 站内搜索（cmsArticleWebOld，按 time 排序）
//  2. Sina 财经搜索（fallback，覆盖 EastMoney 漏抓的题材）
//  3. 失败的源自动跳过，不抛错
func (f *NewsFetcher) FetchMulti(keywords []string, limit int) []NewsItem {
	if limit <= 0 {
		return nil
	}
	if limit > 30 {
		limit = 30
	}
	bag := make(map[string]NewsItem)
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		for _, item := range f.fromEastMoney(kw, limit) {
			if _, ok := bag[item.URL]; !ok && item.URL != "" {
				bag[item.URL] = item
			}
		}
		for _, item := range f.fromSina(kw, limit) {
			key := item.URL
			if key == "" {
				key = item.Title
			}
			if _, ok := bag[key]; !ok {
				bag[key] = item
			}
		}
	}
	out := make([]NewsItem, 0, len(bag))
	for _, v := range bag {
		out = append(out, v)
	}
	// 按 Date 字段（"2026-05-25 19:27:02" 字典序即时间序）倒序
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// fromEastMoney 调用 EastMoney 站内搜索 JSONP 接口。
// param 形如：
//
//	{"uid":"","keyword":"芯片","type":["cmsArticleWebOld"],
//	 "client":"web","clientType":"web","clientVersion":"curr",
//	 "param":{"cmsArticleWebOld":{"searchScope":"default","sort":"time",
//	   "pageIndex":1,"pageSize":10,"preTag":"<em>","postTag":"</em>"}}}
func (f *NewsFetcher) fromEastMoney(keyword string, limit int) []NewsItem {
	payload := fmt.Sprintf(
		`{"uid":"","keyword":%q,"type":["cmsArticleWebOld"],"client":"web","clientType":"web","clientVersion":"curr","param":{"cmsArticleWebOld":{"searchScope":"default","sort":"time","pageIndex":1,"pageSize":%d,"preTag":"<em>","postTag":"</em>"}}}`,
		keyword, limit,
	)
	u := "https://search-api-web.eastmoney.com/search/jsonp?cb=jQuery&param=" + url.QueryEscape(payload)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://so.eastmoney.com/")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if i := strings.Index(text, "("); i >= 0 {
		if j := strings.LastIndex(text, ")"); j > i {
			text = text[i+1 : j]
		}
	}
	var raw struct {
		Result struct {
			Articles []struct {
				Date      string `json:"date"`
				Title     string `json:"title"`
				Content   string `json:"content"`
				MediaName string `json:"mediaName"`
				URL       string `json:"url"`
			} `json:"cmsArticleWebOld"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil
	}
	items := make([]NewsItem, 0, len(raw.Result.Articles))
	for _, a := range raw.Result.Articles {
		items = append(items, NewsItem{
			Date: a.Date, Title: stripHL(a.Title), Content: stripHL(a.Content),
			Source: nonEmpty(a.MediaName, "东方财富"), URL: a.URL,
		})
	}
	return items
}

// fromSina 调用新浪财经站内搜索（fallback）。
// 格式：https://search.sina.com.cn/?q=芯片&c=news&range=title&col=&source=&from=channel
//
// 这里直接走 Sina 的开放 JSON 接口（搜索 box）：
//
//	https://interface.sina.cn/news/wap/fymobile.d.json?cat_1=finance&page=1&num=10
//	或 search-news.sinajs.cn 等。实测 sina 大部分接口需要 JSONP + Referer。
//
// 简化实现：仅作占位 stub，未来可接入真实 sina 端点；当前返回 nil 即可。
// EastMoney 已经覆盖大多数 A 股板块，sina 留作后续扩展点。
func (f *NewsFetcher) fromSina(keyword string, limit int) []NewsItem {
	_ = keyword
	_ = limit
	return nil
}

func stripHL(s string) string {
	s = strings.ReplaceAll(s, "<em>", "")
	s = strings.ReplaceAll(s, "</em>", "")
	return strings.TrimSpace(s)
}

func nonEmpty(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
