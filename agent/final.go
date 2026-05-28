package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/eino-multi-etf-strategy/llm"
	"github.com/eino-multi-etf-strategy/types"
)

type FinalAgent struct {
	LLM llm.Client
}

func NewFinalAgent(c llm.Client) *FinalAgent {
	return &FinalAgent{LLM: c}
}

const finalSystemPrompt = `你是一名顶级量化策略经理。你将收到 6 个子 Agent 的分析输入：
1) screener：量化筛选得分（指标、Top5、最佳标的）
2) news：板块消息面情绪
3) global：海外（美股前夜+日韩盘中）传导
4) tech：技术面研判（趋势/MA/MACD/RSI/支撑压力）
5) regime：宏观环境过滤（沪深300 趋势/回撤/建议仓位上限）
6) money_flow：资金面（北向、ETF 申赎、主力资金代理估算）

【关键认知 — 因子相关性】
不同板块的子因子物理意义差异巨大。你必须先识别"目标 ETF 的因子相关性 profile"，再做决策：
- 海外 ETF（513520 日经/513100 纳指/513030 德国30 等）：标的资产在境外交易，
  · 北向资金 / 沪深300 宏观 对其影响极弱（A 股资金面≠日股资金面）；
  · 海外指数(SPX/N225/KOSPI/HSI) 当日走势是核心驱动；
  · ETF 自身的折溢价、量比、技术面反映场内供需，仍重要；
  · "money_flow" 中的"北向/主力代理"对此类标的属于低权重信号，分析时不得作为关键论据。
- 港股 ETF：南向资金 + 港股流动性 + 美股传导驱动，宏观影响中等。
- A 股科技/新能源：北向资金 + 产业政策(News) + 宏观风险偏好共同作用。
- 顺周期/宽基：宏观 Regime 权重抬高。
- 贵金属/债券：宏观利率与海外避险情绪驱动，动量信号要打折。

【系统已为你计算的"板块自适应权重"】
用户输入中含 "weights" 字段（如 海外：Quant=.30 Tech=.30 News=.05 Global=.25 Regime=.05 Flow=.05）。
你必须严格按此权重做加权综合，不得自行调整权重。

【综合评分 → recommendation】
- overall_score = Σ weight_i × factor_score_i
- 映射： >=80 strong_buy / >=65 buy / >=50 hold / 否则 avoid
- 当 regime.trend == "risk_off" 时强制 avoid；regime.trend == "bear" 时降一档（strong_buy/buy → hold）
- 当 target.premium_pct >= 0.03（+3%）时，strong_buy/buy 强制降为 hold；当 +1.5%~+3% 时，strong_buy 降为 buy（高溢价 = 场内追高，IOPV 跟不上 → 回归净值的下行风险大）
- 仓位上限不得超过 regime.position_cap

【价格方案】
- entry_price：当前价附近，结合海外联动判断高/低开预期
- stop_loss：MA20 / MA60 之间最近支撑下方 1%
- take_profit：基于 ATR 或前高，建议 4%-8% 区间

【reasoning 三段式约束】
① 整体逻辑（必须显式说明"本板块因子相关性 profile"，例如"作为海外 ETF，海外联动是核心、A 股资金面影响弱"）
② 关键风险（务必结合 News 真实标题 + Tech 真实指标 + Regime 真实数据；禁止编造"北向资金净流出"作为日经 ETF 风险点）
③ 操作要点（含仓位建议，需结合 position_cap）

仅输出 JSON：
{
  "overall_score": 0-100 数值,
  "recommendation": "strong_buy | buy | hold | avoid",
  "entry_price": 数字,
  "stop_loss": 数字,
  "take_profit": 数字,
  "reasoning": "<=350 字三段式"
}
约束：
- 任一 Agent 不可信时降低权重；
- 当 regime 为 risk_off 时给 avoid 并在 reasoning 中说明；
- reasoning 中提到的"数字 / 政策 / 资金流"必须能从用户输入中找到出处，不得编造。`

func (a *FinalAgent) Run(ctx context.Context, st *types.AgentState) (*types.FinalDecision, error) {
	if st.Screener == nil || len(st.Screener.Top5) == 0 {
		return nil, fmt.Errorf("no candidate ETF")
	}
	target := st.Screener.Best
	weights := WeightsForSector(target.ETF.Sector)
	relevance := FactorRelevanceNote(target.ETF.Sector)

	payload, _ := json.MarshalIndent(map[string]interface{}{
		"target": map[string]interface{}{
			"code":         target.ETF.Code,
			"name":         target.ETF.Name,
			"sector":       target.ETF.Sector,
			"price":        target.ETF.Price,
			"iopv":         target.ETF.IOPV,
			"premium_pct":  target.ETF.PremiumPct,
			"premium_risk": PremiumRiskLabel(target.ETF.PremiumPct),
			"score":        target.Score,
			"indicators":   target.Indicators,
		},
		"news":       st.News,
		"global":     st.Global,
		"tech":       st.Tech,
		"regime":     st.Regime,
		"money_flow": st.MoneyFlow,
		"weights": map[string]float64{
			"quant":  weights.Quant,
			"tech":   weights.Tech,
			"news":   weights.News,
			"global": weights.Global,
			"regime": weights.Regime,
			"flow":   weights.Flow,
		},
		"factor_relevance_note": relevance,
	}, "", "  ")
	user := fmt.Sprintf("以下是 6 个子 Agent 的输入 + 板块自适应权重 + 因子相关性提示，请输出最终决策：\n%s", string(payload))

	dec := &types.FinalDecision{
		TargetETF:      target,
		NewsAnalysis:   deref(st.News),
		GlobalAnalysis: derefG(st.Global),
		TechAnalysis:   derefT(st.Tech),
		GeneratedAt:    time.Now(),
	}

	err := callLLMJSON(ctx, a.LLM, finalSystemPrompt, user, dec, func(raw string) {
		if dec.Reasoning == "" {
			dec.Reasoning = raw
		}
	})
	if err != nil {
		ruleBasedFinal(dec, st)
		return dec, nil
	}

	// 缺失字段补齐
	if dec.OverallScore == 0 {
		dec.OverallScore = weightedScore(st)
	}
	if dec.Recommendation == "" {
		dec.Recommendation = ruleRecommend(dec.OverallScore)
	}
	dec.Recommendation = CapRecommendation(dec.Recommendation, st.Regime)
	if capped, note := CapByPremium(dec.Recommendation, target.ETF.PremiumPct); note != "" {
		dec.Recommendation = capped
		if dec.Reasoning != "" {
			dec.Reasoning += " "
		}
		dec.Reasoning += "【溢价风险】" + note + "。"
	}
	if dec.EntryPrice == 0 {
		dec.EntryPrice = target.ETF.Price
	}
	if dec.StopLoss == 0 {
		dec.StopLoss = defaultStopLoss(target)
	}
	if dec.TakeProfit == 0 {
		dec.TakeProfit = defaultTakeProfit(target)
	}
	dec.TargetETF = target
	dec.NewsAnalysis = deref(st.News)
	dec.GlobalAnalysis = derefG(st.Global)
	dec.TechAnalysis = derefT(st.Tech)
	dec.GeneratedAt = time.Now()
	dec.ScoreBreakdown = scoreBreakdown(st)
	return dec, nil
}

func ruleBasedFinal(dec *types.FinalDecision, st *types.AgentState) {
	RuleBasedDecision(dec, st)
}

// RuleBasedDecision 暴露规则版决策，供回测引擎复用（不依赖 LLM）。
func RuleBasedDecision(dec *types.FinalDecision, st *types.AgentState) {
	target := st.Screener.Best
	dec.OverallScore = weightedScore(st)
	dec.Recommendation = ruleRecommend(dec.OverallScore)
	dec.Recommendation = CapRecommendation(dec.Recommendation, st.Regime)
	premiumNote := ""
	if capped, note := CapByPremium(dec.Recommendation, target.ETF.PremiumPct); note != "" {
		dec.Recommendation = capped
		premiumNote = note
	}
	dec.EntryPrice = target.ETF.Price
	dec.StopLoss = defaultStopLoss(target)
	dec.TakeProfit = defaultTakeProfit(target)
	dec.ScoreBreakdown = scoreBreakdown(st)
	w := WeightsForSector(target.ETF.Sector)
	premiumDesc := ""
	if target.ETF.IOPV > 0 {
		premiumDesc = fmt.Sprintf(" · 溢价率%+.2f%%(%s)",
			target.ETF.PremiumPct*100, PremiumRiskLabel(target.ETF.PremiumPct))
	}
	dec.Reasoning = fmt.Sprintf(
		"规则版决策（板块=%s 自适应权重 Q%.0f%%/T%.0f%%/N%.0f%%/G%.0f%%/R%.0f%%/F%.0f%%）：量化%.1f / 技术%.1f / 消息%.1f / 海外%.1f / 宏观%.1f / 资金%.1f → 综合 %.1f，建议 %s%s。仓位上限 %.0f%%。【因子相关性】%s",
		target.ETF.Sector, w.Quant*100, w.Tech*100, w.News*100, w.Global*100, w.Regime*100, w.Flow*100,
		target.Score, scoreOrT(st.Tech, 50), scoreOr(st.News, 50), scoreOrG(st.Global, 50),
		scoreOrR(st.Regime, 50), scoreOrM(st.MoneyFlow, 50),
		dec.OverallScore, dec.Recommendation, premiumDesc,
		positionCap(st.Regime)*100,
		FactorRelevanceNote(target.ETF.Sector),
	)
	if premiumNote != "" {
		dec.Reasoning += " 【溢价风险】" + premiumNote + "。"
	}
}

func weightedScore(st *types.AgentState) float64 {
	w := WeightsForSector(st.Screener.Best.ETF.Sector)
	q := st.Screener.Best.Score
	n := scoreOr(st.News, 50)
	g := scoreOrG(st.Global, 50)
	t := scoreOrT(st.Tech, 50)
	r := scoreOrR(st.Regime, 50)
	m := scoreOrM(st.MoneyFlow, 50)
	return w.Quant*q + w.Tech*t + w.News*n + w.Global*g + w.Regime*r + w.Flow*m
}

// scoreBreakdown 把加权分数拆开返回，便于在报告中展示。
func scoreBreakdown(st *types.AgentState) map[string]float64 {
	w := WeightsForSector(st.Screener.Best.ETF.Sector)
	q := st.Screener.Best.Score
	n := scoreOr(st.News, 50)
	g := scoreOrG(st.Global, 50)
	t := scoreOrT(st.Tech, 50)
	r := scoreOrR(st.Regime, 50)
	m := scoreOrM(st.MoneyFlow, 50)
	return map[string]float64{
		"quant":         q,
		"quant_weight":  w.Quant,
		"quant_part":    w.Quant * q,
		"news":          n,
		"news_weight":   w.News,
		"news_part":     w.News * n,
		"global":        g,
		"global_weight": w.Global,
		"global_part":   w.Global * g,
		"tech":          t,
		"tech_weight":   w.Tech,
		"tech_part":     w.Tech * t,
		"regime":        r,
		"regime_weight": w.Regime,
		"regime_part":   w.Regime * r,
		"flow":          m,
		"flow_weight":   w.Flow,
		"flow_part":     w.Flow * m,
	}
}

func defaultStopLoss(target types.ScoredETF) float64 {
	ma20 := target.Indicators["MA20"]
	if ma20 > 0 && ma20 < target.ETF.Price {
		return ma20 * 0.99
	}
	return target.ETF.Price * 0.97
}

func defaultTakeProfit(target types.ScoredETF) float64 {
	vol := target.Indicators["Volatility"]
	if vol > 0 {
		return target.ETF.Price * (1 + clamp(vol*5, 0.04, 0.10))
	}
	return target.ETF.Price * 1.05
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func ruleRecommend(s float64) string {
	switch {
	case s >= 80:
		return "strong_buy"
	case s >= 65:
		return "buy"
	case s >= 50:
		return "hold"
	default:
		return "avoid"
	}
}

func deref(n *types.NewsAnalysis) types.NewsAnalysis {
	if n == nil {
		return types.NewsAnalysis{}
	}
	return *n
}
func derefG(n *types.GlobalMarketAnalysis) types.GlobalMarketAnalysis {
	if n == nil {
		return types.GlobalMarketAnalysis{}
	}
	return *n
}
func derefT(n *types.TechnicalAnalysis) types.TechnicalAnalysis {
	if n == nil {
		return types.TechnicalAnalysis{}
	}
	return *n
}
func scoreOr(n *types.NewsAnalysis, def float64) float64 {
	if n == nil {
		return def
	}
	return n.Score
}
func scoreOrG(n *types.GlobalMarketAnalysis, def float64) float64 {
	if n == nil {
		return def
	}
	return n.Score
}
func scoreOrT(n *types.TechnicalAnalysis, def float64) float64 {
	if n == nil {
		return def
	}
	return n.Score
}
func scoreOrR(n *types.RegimeAnalysis, def float64) float64 {
	if n == nil {
		return def
	}
	return n.Score
}
func scoreOrM(n *types.MoneyFlowAnalysis, def float64) float64 {
	if n == nil {
		return def
	}
	return n.Score
}
func positionCap(r *types.RegimeAnalysis) float64 {
	if r == nil {
		return 0.5
	}
	return r.PositionCap
}
