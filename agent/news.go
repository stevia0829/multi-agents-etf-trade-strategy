package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/eino-multi-etf-strategy/datasource"
	"github.com/eino-multi-etf-strategy/llm"
	"github.com/eino-multi-etf-strategy/types"
)

// NewsAgent 板块消息面研判 Agent。
//
// 设计参考 ValueCell 项目（github.com/ValueCell-ai/valuecell）的 news_agent：
//   - 工具前置：先用 NewsFetcher 真实抓取板块/标的的最新新闻（EastMoney 站内搜索）；
//   - LLM 仅做摘要：把抓到的真实新闻列表塞进 user prompt，让 LLM 提炼情绪/催化剂/风险，
//     而非凭空捏造；
//   - 强约束输出：禁止编造没有出现在抓取结果中的事实；
//   - 兜底真实化：LLM 不可达时基于真实新闻标题做规则推断，不再"按板块名瞎猜"。
type NewsAgent struct {
	LLM     llm.Client
	Fetcher *datasource.NewsFetcher
	// MaxItems 单次抓取的最大新闻条数，默认 8。
	MaxItems int
}

func NewNewsAgent(c llm.Client) *NewsAgent {
	return &NewsAgent{LLM: c, Fetcher: datasource.NewNewsFetcher(), MaxItems: 8}
}

const newsSystemPrompt = `你是一名拥有 15 年卖方经验的"首席行业研究员"，
风格融合彼得·林奇（Peter Lynch）的"实地调研、用常识做生意"与
查理·芒格（Charlie Munger）的"多元思维模型 / 反向思考"。
你拿到的每一条"真实新闻"都来自系统已抓取的最新公开资讯，禁止编造未列出的公司名称、数字、政策。

【你的研究纪律】
- 林奇视角：把每一条新闻翻译成"这家板块在赚什么钱、谁在买单、订单是否真实"。
- 芒格视角：始终问"如果我是反方，会怎么打这个故事的脸"，并在 risks 中体现。
- 价值派纪律：对未经核实的传闻一律标注"待核实"，禁止给出确定性结论。

工作流：
1) 阅读"真实新闻列表"中所有标题与摘要；
2) 抽取真实出现的：催化剂(catalysts) / 风险点(risks) / 资金面信号(flows)；
3) 综合给出 sentiment / score / 200 字 summary（必须基于真实新闻原文）；
4) highlight 数组的每一条都必须能在新闻列表中找到出处，不可虚构。

约束：
- 严禁出现"据悉/据传/某券商认为/可能/预计"等无来源虚词；
- 缺少新闻时，必须在 summary 中明示"近期公开资讯有限"，不得编造；
- 仅输出严格 JSON，禁止 markdown，禁止解释。

JSON Schema:
{
  "sector": "板块中文名",
  "sentiment": "positive | neutral | negative",
  "score": 0-100,
  "highlight": ["要点1","要点2"]   // 每条 < 30 字，必须基于真实新闻原文
  "summary": "<=200 字，包含催化剂 / 风险 / 资金面三段式，全部基于真实新闻"
}`

// Run 流程：
//  1. 多关键词抓真实新闻（板块 + ETF 名）；
//  2. 把新闻列表塞 prompt → LLM 摘要；
//  3. LLM 失败 → 用真实新闻列表 + 板块情绪规则给出兜底。
func (a *NewsAgent) Run(ctx context.Context, etf types.ScoredETF) (*types.NewsAnalysis, error) {
	keywords := buildNewsKeywords(etf)
	limit := a.MaxItems
	if limit <= 0 {
		limit = 8
	}
	news := a.Fetcher.FetchMulti(keywords, limit)

	user := fmt.Sprintf(
		"目标 ETF: %s(%s)\n板块: %s\n当前价: %.3f\n近 20 日动量: %.2f%%\n\n[真实新闻列表（按时间倒序，共 %d 条）]\n%s\n\n请基于以上真实新闻输出板块情绪研判（按 JSON Schema）。",
		etf.ETF.Name, etf.ETF.Code, etf.ETF.Sector,
		etf.ETF.Price, etf.Indicators["Momentum20"]*100,
		len(news), formatNewsList(news),
	)

	res := &types.NewsAnalysis{Sector: etf.ETF.Sector, ETFCode: etf.ETF.Code, ETFName: etf.ETF.Name}
	err := callLLMJSON(ctx, a.LLM, newsSystemPrompt, user, res, func(raw string) {
		if res.Summary == "" {
			res.Summary = raw
		}
	})
	if err != nil || res.Sentiment == "" {
		fb := ruleBasedNewsFromItems(etf, news)
		fb.ETFCode = etf.ETF.Code
		fb.ETFName = etf.ETF.Name
		return fb, nil
	}
	if res.Score == 0 {
		res.Score = mapSentimentScore(res.Sentiment)
	}
	if res.Sector == "" {
		res.Sector = etf.ETF.Sector
	}
	res.ETFCode = etf.ETF.Code
	res.ETFName = etf.ETF.Name
	return res, nil
}

// RunTop 批量分析 Top5（按 etfs 顺序返回 NewsAnalysis 切片）。
// 单条失败不影响其他条；全部失败也会返回 nil 切片以外的兜底结果。
func (a *NewsAgent) RunTop(ctx context.Context, etfs []types.ScoredETF) []types.NewsAnalysis {
	out := make([]types.NewsAnalysis, 0, len(etfs))
	for _, e := range etfs {
		select {
		case <-ctx.Done():
			return out
		default:
		}
		r, err := a.Run(ctx, e)
		if err != nil || r == nil {
			continue
		}
		out = append(out, *r)
	}
	return out
}

// buildNewsKeywords 多关键词扇出：板块名 / ETF 名（去掉"ETF"后缀）/ 核心概念别名。
func buildNewsKeywords(etf types.ScoredETF) []string {
	kw := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			kw[s] = struct{}{}
		}
	}
	add(etf.ETF.Sector)
	name := strings.TrimSuffix(etf.ETF.Name, "ETF")
	name = strings.TrimSuffix(name, "etf")
	add(name)
	// 板块同义词扩展
	switch etf.ETF.Sector {
	case "科技":
		add("半导体")
		add("芯片")
	case "医药":
		add("生物医药")
	case "新能源":
		add("光伏")
		add("锂电池")
	case "金融":
		add("券商")
		add("银行")
	case "消费":
		add("白酒")
	}
	out := make([]string, 0, len(kw))
	for k := range kw {
		out = append(out, k)
	}
	return out
}

func formatNewsList(items []datasource.NewsItem) string {
	if len(items) == 0 {
		return `（接口未返回新闻；请在 summary 中明示"近期公开资讯有限"。）`
	}
	var b strings.Builder
	for i, it := range items {
		title := it.Title
		if len(title) > 60 {
			title = title[:60] + "…"
		}
		content := strings.ReplaceAll(it.Content, "\n", " ")
		if len(content) > 120 {
			content = content[:120] + "…"
		}
		fmt.Fprintf(&b, "%d. [%s | %s] %s\n   摘要: %s\n", i+1, it.Date, it.Source, title, content)
	}
	return b.String()
}

// ruleBasedNewsFromItems 在 LLM 失败时，用真实新闻标题做朴素情绪打分。
//
// 规则：
//   - 含"涨/上涨/创新高/突破/利好/政策/扶持/订单/中标" → +1
//   - 含"跌/下跌/亏损/利空/退市/调查/处罚/警示/风险" → -1
//   - 综合得分映射到 sentiment/score
//   - highlight 取最多 3 条真实标题
func ruleBasedNewsFromItems(etf types.ScoredETF, items []datasource.NewsItem) *types.NewsAnalysis {
	if len(items) == 0 {
		return &types.NewsAnalysis{
			Sector: etf.ETF.Sector, Sentiment: "neutral", Score: 50,
			Highlight: []string{"近期公开资讯有限，建议人工复核"},
			Summary:   fmt.Sprintf("板块 %s 近期未抓取到公开新闻，按中性处理。", etf.ETF.Sector),
		}
	}
	pos := []string{"涨", "上涨", "创新高", "突破", "利好", "政策", "扶持", "订单", "中标", "增长", "扩产", "回暖", "提速"}
	neg := []string{"跌", "下跌", "亏损", "利空", "退市", "调查", "处罚", "警示", "风险", "下滑", "裁员", "减持", "暴跌"}
	score := 0
	for _, it := range items {
		text := it.Title + it.Content
		for _, w := range pos {
			if strings.Contains(text, w) {
				score++
				break
			}
		}
		for _, w := range neg {
			if strings.Contains(text, w) {
				score--
				break
			}
		}
	}
	sentiment := "neutral"
	scoreF := 50.0
	switch {
	case score >= 2:
		sentiment, scoreF = "positive", clamp01_100(55+float64(score)*5)
	case score <= -2:
		sentiment, scoreF = "negative", clamp01_100(45+float64(score)*5)
	}
	highlight := make([]string, 0, 3)
	for i := 0; i < len(items) && i < 3; i++ {
		t := items[i].Title
		if len(t) > 28 {
			t = t[:28]
		}
		highlight = append(highlight, t)
	}
	return &types.NewsAnalysis{
		Sector:    etf.ETF.Sector,
		Sentiment: sentiment,
		Score:     scoreF,
		Highlight: highlight,
		Summary: fmt.Sprintf(
			"LLM 不可达，基于 %d 条真实新闻标题做规则推断：净情绪得分 %d → %s。Top 标题：%s。",
			len(items), score, sentiment, strings.Join(highlight, " / "),
		),
	}
}

func mapSentimentScore(s string) float64 {
	switch s {
	case "positive":
		return 70
	case "negative":
		return 35
	default:
		return 50
	}
}
