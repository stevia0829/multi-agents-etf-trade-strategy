package agent

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/multi-agents-etf-trade-strategy/datasource"
	"github.com/multi-agents-etf-trade-strategy/llm"
	"github.com/multi-agents-etf-trade-strategy/types"
)

type GlobalMarketAgent struct {
	LLM     llm.Client
	Fetcher *datasource.IndexFetcher
}

func NewGlobalMarketAgent(c llm.Client) *GlobalMarketAgent {
	return &GlobalMarketAgent{LLM: c, Fetcher: datasource.NewIndexFetcher()}
}

const globalSystemPrompt = `你是一名"跨境宏观策略师"，模板取自斯坦利·德鲁肯米勒 (Stanley Druckenmiller) 与
雷·达利欧 (Ray Dalio) 的"全天候 + 风险偏好转盘"框架。
请基于"系统抓取的真实指数行情"评估对今天 A 股目标 ETF 开盘的传导影响。

【你的策略纪律】
- 德鲁肯米勒视角：抓住流动性 / 风险偏好的拐点；夜盘已发生的事实优先于隔日预期。
- 达利欧视角：把指数涨跌映射到 4 大经济周期象限（增长/通胀的 + / -）；不让单一指数主导判断。
- 强约束：所有数字必须直接引用用户输入中"真实指数行情"块，不得编造。

分析维度：
1) 风险偏好通道：标普500 / 纳指 / 道指 收盘涨跌 → 全球风险偏好。
2) 行业映射：美股相关板块 → A 股对应板块（例：纳指科技 → 芯片/科技 ETF；油气 → 能源；银行 → 金融等）。
3) 亚太情绪：日经225 / KOSPI 当日盘中走势是否抢跑全球风险偏好。
4) 港股恒生：作为离岸资金情绪的同步参考。
5) 给出 sentiment / score / 200 字 summary，并在 summary 中给出"开盘是否高开/低开/震荡"的判断。

约束：
- 必须严格使用用户输入中"真实指数行情"块的数字，不得自行编造数字；
- 若某指数标记为 [unavailable]，summary 中必须显式指出该项数据缺失，不得用0涨跌代替；
- previous_session 是最近交易日数据，不能称为今日盘中；引用数据时保留交易日和来源；
- 仅输出严格 JSON，禁止 markdown，禁止解释；
- 数字字段保留 2 位小数；change_pct 用百分比数值（例如 +0.85 表示 +0.85%）。

JSON Schema:
{
  "us_prev":  {"index":"SPX","change":数字,"change_pct":数字,"note":"<=20字"},
  "jp_today": {"index":"N225","change":数字,"change_pct":数字,"note":"<=20字"},
  "kr_today": {"index":"KOSPI","change":数字,"change_pct":数字,"note":"<=20字"},
  "sentiment": "positive | neutral | negative",
  "score": 0-100,
  "summary": "<=200 字综合传导研判，必须基于真实数据"
}`

// Run 流程：
//  1. 先用 IndexFetcher 拉真实指数（SPX / N225 / KOSPI / HSI），
//  2. 把真实数字组装进 user prompt，让 LLM 仅做"摘要 + 板块传导"层面的判断；
//  3. LLM 失败时，直接根据真实指数数字做规则推断（不再凭空填写）。
func (a *GlobalMarketAgent) Run(ctx context.Context, etf types.ScoredETF) (*types.GlobalMarketAnalysis, error) {
	mapping := sectorToOverseaMapping(etf.ETF.Sector)
	quotes := a.Fetcher.FetchIndex([]string{"SPX", "NDX", "DJI", "N225", "KOSPI", "HSI"})

	// 语义日历过滤：基于查询锚点 AsOf
	//   - jp_today / kr_today / hsi_today  必须是 AsOf 当日数据，否则丢弃（因为"今日"还没开盘 / 非交易日）
	//   - us_prev (SPX/NDX/DJI)            必须严格早于 AsOf 当日，否则丢弃（防止把当日盘中误读成"昨日收盘"）
	asOf := a.Fetcher.AsOf
	if !asOf.IsZero() {
		applyCalendarSemantics(quotes, asOf)
	}

	user := fmt.Sprintf(
		"目标 ETF: %s(%s)\n板块: %s\n海外映射板块: %s\n\n[真实指数行情]\n%s\n\n请基于以上真实数字研判该 ETF 的开盘传导。",
		etf.ETF.Name, etf.ETF.Code, etf.ETF.Sector, mapping,
		formatIndexQuotes(quotes),
	)

	res := &types.GlobalMarketAnalysis{}
	err := callLLMJSON(ctx, a.LLM, globalSystemPrompt, user, res, func(raw string) {
		if res.Summary == "" {
			res.Summary = raw
		}
	})
	if err != nil || res.Sentiment == "" {
		return ruleBasedGlobalFromQuotes(quotes), nil
	}
	// 用真实数字覆盖 LLM 字段，防止 LLM 回填错数
	overrideSnapshotsWithReal(res, quotes)
	if res.Score == 0 {
		res.Score = mapSentimentScore(res.Sentiment)
	}
	return res, nil
}

func sectorToOverseaMapping(sector string) string {
	m := map[string]string{
		"科技":  "纳指 / 费城半导体 SOX / 韩国半导体（三星/SK海力士）",
		"医药":  "标普医疗保健 / 纳指生物科技",
		"新能源": "纳指特斯拉链 / 韩国电池股 / 日本电装",
		"金融":  "标普金融 / 道指银行股",
		"消费":  "标普可选消费 / 必选消费",
		"军工":  "标普工业 / 国防航空",
		"地产":  "标普房地产 REITs",
		"宽基":  "标普500 / 纳指 / 道指 / 日经 / KOSPI 综合",
	}
	if v, ok := m[sector]; ok {
		return v
	}
	return strings.Join([]string{"标普500", "纳指", "日经225", "KOSPI"}, " / ")
}

// formatIndexQuotes 把抓到的真实行情格式化成 LLM 可读表格。
func formatIndexQuotes(quotes map[string]datasource.IndexQuote) string {
	order := []string{"SPX", "NDX", "DJI", "N225", "KOSPI", "HSI"}
	labels := map[string]string{
		"SPX":   "美股前一日 标普500",
		"NDX":   "美股前一日 纳指100",
		"DJI":   "美股前一日 道指",
		"N225":  "日经225",
		"KOSPI": "韩国KOSPI",
		"HSI":   "恒生指数",
	}
	var b strings.Builder
	for _, k := range order {
		q, ok := quotes[k]
		label := labels[k]
		if !ok || q.Last <= 0 {
			fmt.Fprintf(&b, "- %s: [unavailable]（%s；请在summary中标注缺失）\n", label, q.Error)
			continue
		}
		fmt.Fprintf(&b, "- %s: 最新=%.2f, 前收盘=%.2f, 涨跌=%+.2f (%+.2f%%), 数据源=%s, 时间=%s, 交易日=%s, 状态=%s\n",
			label, q.Last, q.PrevClose, q.Change, q.ChangePct*100,
			q.Source, q.Time.Format(time.RFC3339), q.TradingDate, q.Status)
	}
	return b.String()
}

// overrideSnapshotsWithReal 用真实数据覆盖 LLM 输出，避免 LLM 又把数字写歪。
func overrideSnapshotsWithReal(res *types.GlobalMarketAnalysis, quotes map[string]datasource.IndexQuote) {
	res.Snapshots = nil
	for _, key := range []string{"SPX", "NDX", "DJI", "N225", "KOSPI", "HSI"} {
		res.Snapshots = append(res.Snapshots, realMarketSnapshot(key, quotes[key]))
	}
	res.USPrev = realMarketSnapshot("SPX", quotes["SPX"])
	res.JPToday = realMarketSnapshot("N225", quotes["N225"])
	res.KRToday = realMarketSnapshot("KOSPI", quotes["KOSPI"])
}

func realMarketSnapshot(key string, q datasource.IndexQuote) types.MarketSnapshot {
	if q.Last <= 0 {
		return types.MarketSnapshot{Index: key, Source: q.Source, Status: "unavailable", Note: "数据缺失：" + q.Error}
	}
	return types.MarketSnapshot{Index: key, Last: q.Last, PrevClose: q.PrevClose, Change: round2(q.Change), ChangePc: round2(q.ChangePct * 100), TradingDate: q.TradingDate, AsOf: q.Time.Format(time.RFC3339), Source: q.Source, Status: q.Status, Note: snapshotNote(q)}
}

func snapshotNote(q datasource.IndexQuote) string {
	switch {
	case q.ChangePct >= 0.01:
		return "明显上涨"
	case q.ChangePct >= 0.002:
		return "小幅上涨"
	case q.ChangePct <= -0.01:
		return "明显下跌"
	case q.ChangePct <= -0.002:
		return "小幅下跌"
	default:
		return "基本走平"
	}
}

// round2 保留两位小数。用 math.Round 而非 int(v*100+0.5)，后者对负数会向 +∞ 偏移
// （如 -1.235 会得 -1.23 而非正确的 -1.24），污染下跌日海外指数的涨跌幅显示。
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// ruleBasedGlobalFromQuotes 在 LLM 失败时，用真实指数数据做规则推断（而不是塞"数据缺失"占位）。
// 规则：取 SPX/NDX/N225/KOSPI 平均涨跌幅
//   - >= +0.5% → positive, score 65 + 涨幅放大
//   - <= -0.5% → negative, score 35 - 跌幅放大
//   - 其他 → neutral
func ruleBasedGlobalFromQuotes(quotes map[string]datasource.IndexQuote) *types.GlobalMarketAnalysis {
	pcts := []float64{}
	notes := []string{}
	for _, k := range []string{"SPX", "NDX", "N225", "KOSPI"} {
		if q := quotes[k]; q.Last > 0 {
			pcts = append(pcts, q.ChangePct)
			notes = append(notes, fmt.Sprintf("%s %+.2f%%", k, q.ChangePct*100))
		}
	}
	avg := 0.0
	for _, p := range pcts {
		avg += p
	}
	if len(pcts) > 0 {
		avg /= float64(len(pcts))
	}
	sentiment := "neutral"
	score := 50.0
	switch {
	case avg >= 0.005:
		sentiment, score = "positive", clamp01_100(65+avg*1000)
	case avg <= -0.005:
		sentiment, score = "negative", clamp01_100(35+avg*1000)
	}
	summary := "LLM 不可达，按真实行情数据规则推断："
	if len(notes) > 0 {
		summary += strings.Join(notes, "; ") + fmt.Sprintf("，平均 %+.2f%% → %s。", avg*100, sentiment)
	} else {
		summary += "所有指数均拉取失败，按中性处理。"
	}
	res := &types.GlobalMarketAnalysis{
		Sentiment: sentiment,
		Score:     score,
		Summary:   summary,
	}
	overrideSnapshotsWithReal(res, quotes)
	return res
}

func clamp01_100(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// applyCalendarSemantics 按真实报价时间校验，保留已收盘美股并排除未来快照。
func applyCalendarSemantics(quotes map[string]datasource.IndexQuote, asOf time.Time) {
	for key, q := range quotes {
		if q.Last <= 0 {
			continue
		}
		if err := datasource.ValidateIndexQuoteAsOf(q, asOf); err != nil {
			quotes[key] = datasource.IndexQuote{Symbol: key, Source: "unavailable", Error: err.Error()}
		}
	}
}
