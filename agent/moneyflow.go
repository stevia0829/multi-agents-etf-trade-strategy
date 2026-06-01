package agent

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/eino-multi-etf-strategy/datasource"
	"github.com/eino-multi-etf-strategy/types"
)

// MoneyFlowAgent 资金流向 Agent。
//
// 设计说明：
//
//	真正的北向 / 主力数据需要付费/有限授权接口。本 Agent 采用"近似估算 + 规则映射"路线，
//	完全基于已有 ETF K 线 + 量比 + 板块代理推导，保证无外部依赖也能给出合理资金面信号。
//	后续若接入真实 dataapi，只需替换内部三个 estimate* 函数即可。
//
// 算法：
//   - 近 5 日量价齐升 → 资金净流入（估算正值）
//   - 量比放大 + 收阴 → 资金分歧 / 派发（估算负值）
//   - 综合 score 0-100，sentiment 由 score 映射
type MoneyFlowAgent struct {
	DS         datasource.ETFDataSource
	HistoryDay int // 默认 30
	AsOf       time.Time
}

func NewMoneyFlowAgent(ds datasource.ETFDataSource) *MoneyFlowAgent {
	return &MoneyFlowAgent{DS: ds, HistoryDay: 30}
}

func (a *MoneyFlowAgent) Run(ctx context.Context, etf types.ScoredETF) (*types.MoneyFlowAnalysis, error) {
	klines := etf.ETF.History
	if len(klines) < 20 {
		k, err := a.DS.GetKLineAsOf(etf.ETF.Code, a.HistoryDay, a.AsOf)
		if err != nil {
			return nil, fmt.Errorf("moneyflow fetch %s: %w", etf.ETF.Code, err)
		}
		klines = k
	}
	if len(klines) < 5 {
		return nil, fmt.Errorf("moneyflow %s real klines insufficient: got %d need >=5", etf.ETF.Code, len(klines))
	}

	res := &types.MoneyFlowAnalysis{ETFCode: etf.ETF.Code}
	res.NorthCapital5d = estimateNorthCapital(klines, 5)
	res.NorthCapital20d = estimateNorthCapital(klines, 20)
	res.ETFNetSubscribe = estimateETFSubscribe(klines, 5)
	res.MainNetInflow3d = estimateMainInflow(klines, 3)

	res.Score = scoreMoneyFlow(res)
	res.Sentiment = sentimentFromScore(res.Score)
	res.Summary = composeMoneyFlowSummary(res)
	return res, nil
}

// estimateNorthCapital 用价格涨幅 × 成交额 × 系数估算"北向资金倾向"。
// 这只是行为代理：真实北向数据应替换此函数。
func estimateNorthCapital(klines []types.KLine, days int) float64 {
	if len(klines) <= days {
		days = len(klines) - 1
	}
	if days <= 0 {
		return 0
	}
	sub := klines[len(klines)-days:]
	total := 0.0
	for i := 0; i < len(sub); i++ {
		change := 0.0
		if i > 0 {
			prev := sub[i-1].Close
			if prev > 0 {
				change = (sub[i].Close - prev) / prev
			}
		}
		// 用涨跌幅 × 成交额作为代理（亿元单位 ≈ volume * close / 1e8）
		amount := sub[i].Volume * sub[i].Close / 1e8
		total += change * amount * 0.05 // 0.05 是经验系数（北向占 ETF 成交比）
	}
	return roundN(total, 2)
}

// estimateETFSubscribe 用量比 × 涨幅估算 ETF 净申购量级（亿元）。
func estimateETFSubscribe(klines []types.KLine, days int) float64 {
	if len(klines) <= days {
		days = len(klines) - 1
	}
	if days <= 0 {
		return 0
	}
	avgVol := 0.0
	baseStart := len(klines) - days - 10
	if baseStart < 0 {
		baseStart = 0
	}
	for i := baseStart; i < len(klines)-days; i++ {
		avgVol += klines[i].Volume
	}
	if div := len(klines) - days - baseStart; div > 0 {
		avgVol /= float64(div)
	}
	if avgVol <= 0 {
		return 0
	}

	total := 0.0
	for i := len(klines) - days; i < len(klines); i++ {
		ratio := klines[i].Volume / avgVol
		change := 0.0
		if i > 0 && klines[i-1].Close > 0 {
			change = (klines[i].Close - klines[i-1].Close) / klines[i-1].Close
		}
		amount := klines[i].Volume * klines[i].Close / 1e8
		total += (ratio - 1) * sign(change) * amount * 0.1
	}
	return roundN(total, 2)
}

// estimateMainInflow 用 (收-开)/(高-低) × 成交额 估算主力净流入。
// 经典 BOP（Balance of Power）变体。
func estimateMainInflow(klines []types.KLine, days int) float64 {
	if len(klines) <= days {
		days = len(klines) - 1
	}
	if days <= 0 {
		return 0
	}
	total := 0.0
	for i := len(klines) - days; i < len(klines); i++ {
		k := klines[i]
		hl := k.High - k.Low
		if hl <= 0 {
			continue
		}
		bop := (k.Close - k.Open) / hl // -1 ~ +1
		amount := k.Volume * k.Close / 1e8
		total += bop * amount * 0.3
	}
	return roundN(total, 2)
}

func scoreMoneyFlow(r *types.MoneyFlowAnalysis) float64 {
	score := 50.0
	score += clampMF(r.NorthCapital5d*5, -15, 15)
	score += clampMF(r.NorthCapital20d*1.5, -10, 10)
	score += clampMF(r.ETFNetSubscribe*8, -10, 10)
	score += clampMF(r.MainNetInflow3d*10, -15, 15)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return roundN(score, 2)
}

func sentimentFromScore(score float64) string {
	switch {
	case score >= 65:
		return "positive"
	case score <= 35:
		return "negative"
	default:
		return "neutral"
	}
}

func composeMoneyFlowSummary(r *types.MoneyFlowAnalysis) string {
	tag := "中性"
	switch r.Sentiment {
	case "positive":
		tag = "净流入"
	case "negative":
		tag = "净流出"
	}
	return fmt.Sprintf(
		"%s：北向资金代理 5 日 %.2f / 20 日 %.2f；ETF 5 日净申购代理 %.2f；主力 3 日净流入代理 %.2f（单位：亿元，估算）。资金面评分 %.0f。",
		tag,
		r.NorthCapital5d, r.NorthCapital20d,
		r.ETFNetSubscribe, r.MainNetInflow3d,
		r.Score,
	)
}

func clampMF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func sign(v float64) float64 {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}

func roundN(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
