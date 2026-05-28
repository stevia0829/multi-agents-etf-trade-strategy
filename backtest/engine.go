package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/eino-multi-etf-strategy/agent"
	"github.com/eino-multi-etf-strategy/datasource"
	"github.com/eino-multi-etf-strategy/types"
)

// Trade 单次回测交易记录。
type Trade struct {
	AsOf           time.Time `json:"as_of"`
	BestCode       string    `json:"best_code"`
	BestName       string    `json:"best_name"`
	Sector         string    `json:"sector"`
	QuantScore     float64   `json:"quant_score"`
	Recommendation string    `json:"recommendation"`
	RegimeTrend    string    `json:"regime_trend"`
	PositionCap    float64   `json:"position_cap"`
	EntryPrice     float64   `json:"entry_price"`
	ExitPrice      float64   `json:"exit_price"`
	HoldDays       int       `json:"hold_days"`
	RawReturnPct   float64   `json:"raw_return_pct"`   // 5 日原始收益（不含仓位）
	WeightedReturn float64   `json:"weighted_return"`  // 仓位加权收益 = RawReturn × PositionCap
	Win            bool      `json:"win"`              // 加权收益 > 0
	V2Switched     bool      `json:"v2_switched"`      // V2 是否把 V3-best 替换成了次优；仅 v3v2 模式
	V2RejectedTop1 string    `json:"v2_rejected_top1"` // V2 拒绝 V3-best 的原因；仅 v3v2 模式
}

// Result 回测汇总。
type Result struct {
	StartDate time.Time         `json:"start_date"`
	EndDate   time.Time         `json:"end_date"`
	HoldDays  int               `json:"hold_days"`
	Total     int               `json:"total"`
	Wins      int               `json:"wins"`
	Losses    int               `json:"losses"`
	WinRate   float64           `json:"win_rate"`
	AvgReturn float64           `json:"avg_return"`
	MaxReturn float64           `json:"max_return"`
	MinReturn float64           `json:"min_return"`
	StdReturn float64           `json:"std_return"`
	Sharpe    float64           `json:"sharpe"`
	ByReco    map[string]Bucket `json:"by_recommendation"`
	ByRegime  map[string]Bucket `json:"by_regime"`
	BySector  map[string]Bucket `json:"by_sector"`
	Trades    []Trade           `json:"trades"`
}

type Bucket struct {
	Count   int     `json:"count"`
	WinRate float64 `json:"win_rate"`
	AvgRet  float64 `json:"avg_return"`
}

// Engine 回测引擎。
//
// 设计要点：
//   - 不调用任何 LLM Agent（News/Global/Technical/Final-LLM），只用 Screener + Regime + MoneyFlow + 规则版 Final。
//     这样保证回测可重复、零成本、纯量化。
//   - 仓位加权：实际收益 = 原始 5 日收益 × regime.PositionCap。
//   - 仅当 recommendation ∈ {strong_buy, buy} 时才记入交易；hold/avoid 视为空仓（不计 Win/Loss）。
//   - Variant 决定使用纯 V3 评分，还是在 V3 之上叠加 V2 4 道闸门过滤。
type Engine struct {
	DS         datasource.ETFDataSource
	Screener   *agent.ScreenerAgent
	Regime     *agent.RegimeAgent
	MoneyFlow  *agent.MoneyFlowAgent
	HoldDays   int    // 持有 N 个交易日后看收益，默认 5
	MaxSamples int    // 最大样本数，默认 60
	Variant    string // "v3" (默认) 或 "v3v2"
	V2Config   agent.V2FilterConfig
	state      *agent.V2State // V2 模式的跨样本状态
}

func NewEngine(ds datasource.ETFDataSource) *Engine {
	return &Engine{
		DS:        ds,
		Screener:  agent.NewScreenerAgent(ds),
		Regime:    agent.NewRegimeAgent(ds),
		MoneyFlow: agent.NewMoneyFlowAgent(ds),
		HoldDays:  5,
		Variant:   "v3",
		V2Config:  agent.DefaultV2FilterConfig(),
		state:     agent.NewV2State(),
	}
}

// Run 在 [start, end] 区间内逐交易日回测。
// step 控制采样间隔（避免相邻日相关性过强），默认 5 个交易日采样一次。
func (e *Engine) Run(ctx context.Context, start, end time.Time, step int) (*Result, error) {
	if step <= 0 {
		step = 5
	}
	if e.HoldDays <= 0 {
		e.HoldDays = 5
	}

	// 用基准 ETF（510300）的历史 K 线来确定有效交易日序列
	baseKlines, err := e.DS.GetKLineAsOf("510300", 500, end)
	if err != nil || len(baseKlines) == 0 {
		return nil, fmt.Errorf("load benchmark klines: %v", err)
	}

	// 过滤交易日范围
	dates := make([]time.Time, 0)
	for _, k := range baseKlines {
		if !k.Date.Before(start) && !k.Date.After(end) {
			dates = append(dates, k.Date)
		}
	}
	if len(dates) < e.HoldDays+1 {
		return nil, fmt.Errorf("not enough trading days: %d", len(dates))
	}

	// 按 step 采样，并预留 HoldDays 用于查看后续收益
	usable := dates[:len(dates)-e.HoldDays]
	samples := make([]time.Time, 0)
	for i := 0; i < len(usable); i += step {
		samples = append(samples, usable[i])
	}
	if e.MaxSamples > 0 && len(samples) > e.MaxSamples {
		samples = samples[len(samples)-e.MaxSamples:]
	}

	res := &Result{
		StartDate: samples[0],
		EndDate:   samples[len(samples)-1],
		HoldDays:  e.HoldDays,
		ByReco:    map[string]Bucket{},
		ByRegime:  map[string]Bucket{},
		BySector:  map[string]Bucket{},
	}

	for _, d := range samples {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		trade, ok := e.runOnce(ctx, d)
		if !ok {
			continue
		}
		res.Trades = append(res.Trades, trade)
	}

	summarize(res)
	return res, nil
}

// runOnce 在单个 asOf 跑一次精简 pipeline，返回 Trade（若该日无可交易标的，返回 ok=false）。
func (e *Engine) runOnce(ctx context.Context, asOf time.Time) (Trade, bool) {
	e.Screener.AsOf = asOf
	e.Regime.AsOf = asOf
	e.MoneyFlow.AsOf = asOf

	scr, err := e.Screener.Run(ctx)
	if err != nil || scr == nil || len(scr.Top5) == 0 {
		return Trade{}, false
	}
	target := scr.Best
	v2Switched := false
	v2RejectedTop1 := ""

	// V2 4 道闸门过滤（仅 v3v2 模式）：在 Top5 上按顺序找第一个允许进入的，作为 best。
	if e.Variant == "v3v2" {
		if e.state == nil {
			e.state = agent.NewV2State()
		}
		e.state.ResetBanToday() // 切换日，先清空
		e.state.CleanupCooldown(asOf)
		allowed, decisions := agent.ApplyV2Filter(scr.Top5, e.V2Config, e.state, asOf)
		// 记录 Top1 是否被拒（最重要的判断维度）
		if len(decisions) > 0 && !decisions[0].Allowed {
			v2RejectedTop1 = decisions[0].Reason
		}
		if len(allowed) == 0 {
			// 4 道闸门全拒 → 视为该日不交易
			fmt.Printf("[v3v2] %s 全部 Top5 被拒（top1拒绝原因=%s）→ 当日空仓\n",
				asOf.Format("2006-01-02"), v2RejectedTop1)
			return Trade{}, false
		}
		newTarget := allowed[0]
		if newTarget.ETF.Code != scr.Best.ETF.Code {
			v2Switched = true
			fmt.Printf("[v3v2] %s V2 把 best 从 %s 替换为 %s（top1拒绝原因=%s）\n",
				asOf.Format("2006-01-02"), scr.Best.ETF.Code, newTarget.ETF.Code, v2RejectedTop1)
		}
		target = newTarget
	}

	regime, _ := e.Regime.Run(ctx)
	flow, _ := e.MoneyFlow.Run(ctx, target)

	st := &types.AgentState{
		Screener:  scr,
		Regime:    regime,
		MoneyFlow: flow,
	}

	// 规则版决策
	dec := &types.FinalDecision{TargetETF: target}
	agent.RuleBasedDecision(dec, st)

	// 关键：entry/exit 必须来自同一次 K 线拉取，确保前复权基准一致。
	// 直接拉 asOf + HoldDays*2 + 缓冲 的窗口，从中找 asOf 对应索引作为 entry，
	// 索引 + HoldDays 作为 exit。
	futureEnd := asOf.AddDate(0, 0, e.HoldDays*2+15)
	unifiedKlines, err := e.DS.GetKLineAsOf(target.ETF.Code, e.HoldDays*2+25, futureEnd)
	if err != nil || len(unifiedKlines) == 0 {
		return Trade{}, false
	}
	entryIdx := indexOnOrAfter(unifiedKlines, asOf)
	if entryIdx < 0 {
		return Trade{}, false
	}
	exitIdx := entryIdx + e.HoldDays
	if exitIdx >= len(unifiedKlines) {
		exitIdx = len(unifiedKlines) - 1
	}
	entry := unifiedKlines[entryIdx].Close
	exit := unifiedKlines[exitIdx].Close
	if entry <= 0 || exit <= 0 {
		return Trade{}, false
	}
	rawRet := (exit - entry) / entry

	posCap := 0.5
	regimeTrend := "neutral"
	if regime != nil {
		posCap = regime.PositionCap
		regimeTrend = regime.Trend
	}
	weighted := rawRet * posCap
	// 只有真正建仓的信号纳入胜率统计
	if dec.Recommendation == "hold" || dec.Recommendation == "avoid" {
		weighted = 0
	}

	// V2 模式下：根据 hold 期内是否触发止盈/止损更新冷却 / 黑名单。
	// 简化：用区间内最大涨幅触止盈、最低点触止损（更严格的逐日轨迹也可，此处取保守近似）。
	if e.Variant == "v3v2" && (dec.Recommendation == "buy" || dec.Recommendation == "strong_buy") {
		hi, lo := entry, entry
		for i := entryIdx; i <= exitIdx && i < len(unifiedKlines); i++ {
			c := unifiedKlines[i].Close
			if c > hi {
				hi = c
			}
			if c < lo {
				lo = c
			}
		}
		hiRet := hi/entry - 1
		loRet := lo/entry - 1
		stoppedOut := hiRet >= e.V2Config.StopProfitTrigger || loRet <= e.V2Config.StopLossTrigger
		if stoppedOut {
			cooldownEnd := asOf.AddDate(0, 0, e.V2Config.CooldownTradeDays*2) // 自然日近似
			e.state.MarkStopOut(target.ETF.Code, asOf, cooldownEnd)
		}
	}

	return Trade{
		AsOf:           asOf,
		BestCode:       target.ETF.Code,
		BestName:       target.ETF.Name,
		Sector:         target.ETF.Sector,
		QuantScore:     target.Score,
		Recommendation: dec.Recommendation,
		RegimeTrend:    regimeTrend,
		PositionCap:    posCap,
		EntryPrice:     entry,
		ExitPrice:      exit,
		HoldDays:       e.HoldDays,
		RawReturnPct:   rawRet,
		WeightedReturn: weighted,
		Win:            weighted > 0,
		V2Switched:     v2Switched,
		V2RejectedTop1: v2RejectedTop1,
	}, true
}

// findClosePriceAfter 找到 asOf 之后第 holdDays 个交易日的收盘价。
func findClosePriceAfter(klines []types.KLine, asOf time.Time, holdDays int) float64 {
	startIdx := -1
	for i, k := range klines {
		if k.Date.After(asOf) || sameDay(k.Date, asOf) {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return 0
	}
	target := startIdx + holdDays
	if target >= len(klines) {
		target = len(klines) - 1
	}
	return klines[target].Close
}

// indexOnOrAfter 返回 K 线序列中第一根 Date >= asOf 的索引；找不到返回 -1。
func indexOnOrAfter(klines []types.KLine, asOf time.Time) int {
	for i, k := range klines {
		if k.Date.After(asOf) || sameDay(k.Date, asOf) {
			return i
		}
	}
	return -1
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func summarize(r *Result) {
	if len(r.Trades) == 0 {
		return
	}
	executed := 0 // 实际建仓次数（hold/avoid 不计）
	sumRet := 0.0
	maxRet, minRet := -1e9, 1e9
	rets := make([]float64, 0, len(r.Trades))
	r.Total = len(r.Trades)
	for _, t := range r.Trades {
		if t.Recommendation == "hold" || t.Recommendation == "avoid" {
			// 空仓样本不计入收益统计，但仍参与"信号正确率"
			continue
		}
		executed++
		if t.Win {
			r.Wins++
		} else {
			r.Losses++
		}
		sumRet += t.WeightedReturn
		rets = append(rets, t.WeightedReturn)
		if t.WeightedReturn > maxRet {
			maxRet = t.WeightedReturn
		}
		if t.WeightedReturn < minRet {
			minRet = t.WeightedReturn
		}
	}
	if executed > 0 {
		r.WinRate = float64(r.Wins) / float64(executed)
		r.AvgReturn = sumRet / float64(executed)
		r.MaxReturn = maxRet
		r.MinReturn = minRet
		// 标准差
		var sumSq float64
		for _, x := range rets {
			sumSq += (x - r.AvgReturn) * (x - r.AvgReturn)
		}
		r.StdReturn = math.Sqrt(sumSq / float64(executed))
		if r.StdReturn > 0 {
			// 简易 Sharpe：年化收益 / 年化波动（假设每次持有 5 日，一年约 50 个非重叠样本）
			r.Sharpe = (r.AvgReturn * 50) / (r.StdReturn * math.Sqrt(50))
		}
	}

	r.ByReco = bucketize(r.Trades, func(t Trade) string { return t.Recommendation })
	r.ByRegime = bucketize(r.Trades, func(t Trade) string { return t.RegimeTrend })
	r.BySector = bucketize(r.Trades, func(t Trade) string { return t.Sector })
}

func bucketize(trades []Trade, key func(Trade) string) map[string]Bucket {
	tmp := map[string]*[]Trade{}
	for i := range trades {
		k := key(trades[i])
		if k == "" {
			k = "未知"
		}
		if _, ok := tmp[k]; !ok {
			s := make([]Trade, 0)
			tmp[k] = &s
		}
		*tmp[k] = append(*tmp[k], trades[i])
	}
	res := map[string]Bucket{}
	for k, v := range tmp {
		if v == nil || len(*v) == 0 {
			continue
		}
		var wins int
		var sum float64
		exec := 0
		for _, t := range *v {
			if t.Recommendation == "hold" || t.Recommendation == "avoid" {
				continue
			}
			exec++
			if t.Win {
				wins++
			}
			sum += t.WeightedReturn
		}
		b := Bucket{Count: len(*v)}
		if exec > 0 {
			b.WinRate = float64(wins) / float64(exec)
			b.AvgRet = sum / float64(exec)
		}
		res[k] = b
	}
	return res
}

// BuildMarkdown 把 Result 渲染成 markdown 报告。
func BuildMarkdown(r *Result) string {
	var b strings.Builder
	b.WriteString("# 多 Agent 策略历史回测报告\n\n")
	b.WriteString(fmt.Sprintf("- 回测区间: `%s` ~ `%s`\n", r.StartDate.Format("2006-01-02"), r.EndDate.Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("- 持有期: **%d** 个交易日\n", r.HoldDays))
	b.WriteString(fmt.Sprintf("- 样本数: %d（其中实际建仓：%d）\n", r.Total, r.Wins+r.Losses))
	b.WriteString("\n## 一、整体表现\n\n")
	b.WriteString("| 指标 | 数值 |\n|---|---|\n")
	b.WriteString(fmt.Sprintf("| 胜率 | %.2f%% |\n", r.WinRate*100))
	b.WriteString(fmt.Sprintf("| 平均收益（仓位加权）| %.2f%% |\n", r.AvgReturn*100))
	b.WriteString(fmt.Sprintf("| 最大单笔收益 | %.2f%% |\n", r.MaxReturn*100))
	b.WriteString(fmt.Sprintf("| 最大单笔亏损 | %.2f%% |\n", r.MinReturn*100))
	b.WriteString(fmt.Sprintf("| 收益标准差 | %.2f%% |\n", r.StdReturn*100))
	b.WriteString(fmt.Sprintf("| 简易 Sharpe | %.2f |\n\n", r.Sharpe))

	b.WriteString("## 二、按 Recommendation 分类\n\n")
	writeBucket(&b, r.ByReco)
	b.WriteString("\n## 三、按宏观环境分类\n\n")
	writeBucket(&b, r.ByRegime)
	b.WriteString("\n## 四、按板块分类\n\n")
	writeBucket(&b, r.BySector)

	b.WriteString("\n## 五、近 20 笔交易明细\n\n")
	b.WriteString("| 日期 | 标的 | 板块 | 综合分 | 建议 | 宏观 | 仓位 | 5日收益 | 加权 | 胜? |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	tail := r.Trades
	if len(tail) > 20 {
		tail = tail[len(tail)-20:]
	}
	for _, t := range tail {
		win := "❌"
		if t.Win {
			win = "✅"
		}
		b.WriteString(fmt.Sprintf("| %s | %s(%s) | %s | %.2f | %s | %s | %.0f%% | %+.2f%% | %+.2f%% | %s |\n",
			t.AsOf.Format("2006-01-02"), t.BestName, t.BestCode, t.Sector,
			t.QuantScore, t.Recommendation, t.RegimeTrend, t.PositionCap*100,
			t.RawReturnPct*100, t.WeightedReturn*100, win))
	}
	return b.String()
}

func writeBucket(b *strings.Builder, m map[string]Bucket) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("| 分类 | 样本 | 胜率 | 平均加权收益 |\n|---|---|---|---|\n")
	for _, k := range keys {
		v := m[k]
		b.WriteString(fmt.Sprintf("| %s | %d | %.2f%% | %+.2f%% |\n",
			k, v.Count, v.WinRate*100, v.AvgRet*100))
	}
}

// BuildCompareMarkdown 渲染 V3 vs V3+V2 的 A/B 对比报告。
func BuildCompareMarkdown(v3, v3v2 *Result) string {
	var b strings.Builder
	b.WriteString("# 多 Agent 策略 A/B 对比回测：V3 vs V3+V2\n\n")
	b.WriteString(fmt.Sprintf("- 回测区间: `%s` ~ `%s`\n", v3.StartDate.Format("2006-01-02"), v3.EndDate.Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("- 持有期: **%d** 个交易日\n\n", v3.HoldDays))

	b.WriteString("## 一、整体对比\n\n")
	b.WriteString("| 指标 | V3（纯量化动量） | V3+V2（叠加 4 道闸门） | Δ |\n|---|---|---|---|\n")
	row := func(label string, av, bv float64, fmtPct bool) {
		var sa, sb, sd string
		if fmtPct {
			sa = fmt.Sprintf("%.2f%%", av*100)
			sb = fmt.Sprintf("%.2f%%", bv*100)
			sd = fmt.Sprintf("%+.2f pp", (bv-av)*100)
		} else {
			sa = fmt.Sprintf("%.2f", av)
			sb = fmt.Sprintf("%.2f", bv)
			sd = fmt.Sprintf("%+.2f", bv-av)
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", label, sa, sb, sd))
	}
	rowInt := func(label string, av, bv int) {
		b.WriteString(fmt.Sprintf("| %s | %d | %d | %+d |\n", label, av, bv, bv-av))
	}
	rowInt("样本数", v3.Total, v3v2.Total)
	rowInt("实际建仓数", v3.Wins+v3.Losses, v3v2.Wins+v3v2.Losses)
	rowInt("胜次", v3.Wins, v3v2.Wins)
	rowInt("败次", v3.Losses, v3v2.Losses)
	row("胜率", v3.WinRate, v3v2.WinRate, true)
	row("平均加权收益", v3.AvgReturn, v3v2.AvgReturn, true)
	row("最大单笔收益", v3.MaxReturn, v3v2.MaxReturn, true)
	row("最大单笔亏损", v3.MinReturn, v3v2.MinReturn, true)
	row("收益标准差", v3.StdReturn, v3v2.StdReturn, true)
	row("简易 Sharpe", v3.Sharpe, v3v2.Sharpe, false)

	b.WriteString("\n## 二、按 Recommendation 分桶对比\n\n")
	writeBucketCompare(&b, v3.ByReco, v3v2.ByReco)
	b.WriteString("\n## 三、按宏观环境分桶对比\n\n")
	writeBucketCompare(&b, v3.ByRegime, v3v2.ByRegime)
	b.WriteString("\n## 四、按板块分桶对比\n\n")
	writeBucketCompare(&b, v3.BySector, v3v2.BySector)

	b.WriteString("\n## 五、V3 近 20 笔交易明细\n\n")
	writeTradeTail(&b, v3.Trades, 20)
	b.WriteString("\n## 六、V3+V2 近 20 笔交易明细\n\n")
	writeTradeTail(&b, v3v2.Trades, 20)

	b.WriteString("\n## 七、结论速览\n\n")
	b.WriteString(deriveCompareConclusion(v3, v3v2))
	return b.String()
}

func writeBucketCompare(b *strings.Builder, av, bv map[string]Bucket) {
	keys := map[string]bool{}
	for k := range av {
		keys[k] = true
	}
	for k := range bv {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	b.WriteString("| 分类 | V3 样本 | V3 胜率 | V3 平均收益 | V3+V2 样本 | V3+V2 胜率 | V3+V2 平均收益 |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, k := range ordered {
		x := av[k]
		y := bv[k]
		b.WriteString(fmt.Sprintf("| %s | %d | %.2f%% | %+.2f%% | %d | %.2f%% | %+.2f%% |\n",
			k, x.Count, x.WinRate*100, x.AvgRet*100, y.Count, y.WinRate*100, y.AvgRet*100))
	}
}

func writeTradeTail(b *strings.Builder, trades []Trade, n int) {
	b.WriteString("| 日期 | 标的 | 板块 | 综合分 | 建议 | 宏观 | 仓位 | 5日收益 | 加权 | 胜? |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	tail := trades
	if len(tail) > n {
		tail = tail[len(tail)-n:]
	}
	for _, t := range tail {
		win := "❌"
		if t.Win {
			win = "✅"
		}
		b.WriteString(fmt.Sprintf("| %s | %s(%s) | %s | %.2f | %s | %s | %.0f%% | %+.2f%% | %+.2f%% | %s |\n",
			t.AsOf.Format("2006-01-02"), t.BestName, t.BestCode, t.Sector,
			t.QuantScore, t.Recommendation, t.RegimeTrend, t.PositionCap*100,
			t.RawReturnPct*100, t.WeightedReturn*100, win))
	}
}

func deriveCompareConclusion(v3, v3v2 *Result) string {
	dWin := (v3v2.WinRate - v3.WinRate) * 100
	dRet := (v3v2.AvgReturn - v3.AvgReturn) * 100
	dStd := (v3v2.StdReturn - v3.StdReturn) * 100
	dSharpe := v3v2.Sharpe - v3.Sharpe
	var b strings.Builder
	b.WriteString(fmt.Sprintf("- 胜率变化：%+.2f pp（V3 %.2f%% → V3+V2 %.2f%%）\n", dWin, v3.WinRate*100, v3v2.WinRate*100))
	b.WriteString(fmt.Sprintf("- 平均加权收益变化：%+.2f pp（V3 %+.2f%% → V3+V2 %+.2f%%）\n", dRet, v3.AvgReturn*100, v3v2.AvgReturn*100))
	b.WriteString(fmt.Sprintf("- 收益波动变化：%+.2f pp（V3 %.2f%% → V3+V2 %.2f%%）\n", dStd, v3.StdReturn*100, v3v2.StdReturn*100))
	b.WriteString(fmt.Sprintf("- Sharpe 变化：%+.2f\n", dSharpe))
	switch {
	case dSharpe > 0.1 && dWin >= 0:
		b.WriteString("- 综合判断：V2 4 道闸门在该区间显著提升风险调整后收益，建议保留启用。\n")
	case dSharpe > 0 && dWin >= -2:
		b.WriteString("- 综合判断：V2 4 道闸门带来温和改善，建议在波动放大时保留。\n")
	case dSharpe < -0.1:
		b.WriteString("- 综合判断：V2 4 道闸门在该区间反而拖累收益，建议放宽参数或仅在熊市启用。\n")
	default:
		b.WriteString("- 综合判断：两者表现接近，需要扩大样本/区间再做结论。\n")
	}
	return b.String()
}
