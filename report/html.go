package report

import (
	"bytes"
	"fmt"
	"html/template"
	"time"

	"github.com/multi-agents-etf-trade-strategy/agent"
	"github.com/multi-agents-etf-trade-strategy/types"
)

type htmlFactor struct {
	Name                            string
	Score, Weight, Adjustment, Part float64
}

type htmlReport struct {
	Title, Generated, AsOf, DataTime, BaseName, Action string
	State                                              *types.AgentState
	Final                                              *types.FinalDecision
	Factors                                            []htmlFactor
	Holds                                              []types.HoldAdvice
}

// BuildHTML 直接从结构化决策生成独立阅读页，不依赖远程字体、脚本或Markdown解析器。
// html/template 对新闻、模型分析、标的名称等外部文本自动转义。
func BuildHTML(state *types.AgentState, now time.Time) (string, error) {
	if state == nil {
		state = &types.AgentState{}
	}
	f := state.Final
	if f == nil {
		f = &types.FinalDecision{}
	}
	v := htmlReport{
		Title: "ETF 策略日报", Generated: now.Format("2006-01-02 15:04:05 MST"),
		BaseName: "etf-report-" + now.Format("20060102-150405"), State: state, Final: f,
		Action: "数据尚不完整，暂不形成执行建议。",
	}
	if state.RunMode == "close-preview" {
		v.Title = "ETF 尾盘策略报告"
	}
	if state.Screener != nil {
		v.AsOf = state.Screener.AsOfDate.Format("2006-01-02")
	}
	if !state.MarketDataTime.IsZero() {
		v.DataTime = state.MarketDataTime.Format("2006-01-02 15:04:05")
	}
	switch f.Recommendation {
	case "avoid":
		v.Action = "暂停新开仓和轮动买入。已有持仓按独立趋势证据和仓位规则复核。"
	case "hold":
		v.Action = "持有观察，暂不加仓；候选价位用于后续复核。"
	case "buy", "strong_buy":
		v.Action = "允许评估买入候选，执行前核对价格、溢价、可用现金与策略仓位上限。"
	}
	for _, row := range []struct{ name, key string }{
		{"量化动量", "quant"}, {"技术面", "tech"}, {"消息面", "news"},
		{"海外联动", "global"}, {"宏观环境", "regime"}, {"资金面", "flow"},
	} {
		b := f.ScoreBreakdown
		if b == nil {
			break
		}
		adj := 1.0
		if row.key == "news" || row.key == "tech" {
			if a, ok := b[row.key+"_adj"]; ok {
				adj = a
			}
		}
		v.Factors = append(v.Factors, htmlFactor{row.name, b[row.key], b[row.key+"_weight"], adj, b[row.key+"_part"]})
	}
	advices := state.HoldAdvices
	if len(advices) == 0 && state.HoldAdvice != nil {
		advices = []types.HoldAdvice{*state.HoldAdvice}
	}
	for _, a := range advices {
		v.Holds = append(v.Holds, agent.GuardHoldAdvice(a, state.Final))
	}
	funcs := template.FuncMap{
		"num": func(n float64) string { return fmt.Sprintf("%.2f", n) },
		"price": func(n float64) string {
			if n <= 0 {
				return "—"
			}
			return fmt.Sprintf("%.3f", n)
		},
		"pct":       func(n float64) string { return fmt.Sprintf("%.0f%%", n*100) },
		"signedPct": func(n float64) string { return fmt.Sprintf("%+.2f%%", n*100) },
		"adj": func(n float64) string {
			if n >= .995 && n <= 1.005 {
				return "—"
			}
			return fmt.Sprintf("%+.1f%%", (n-1)*100)
		},
		"bar": func(n float64) string {
			if n < 0 {
				n = 0
			}
			if n > 100 {
				n = 100
			}
			return fmt.Sprintf("%.2f", n)
		},
		"label": func(s string) string {
			switch s {
			case "avoid":
				return "回避 · 暂停新增"
			case "hold":
				return "持有观察"
			case "buy":
				return "买入候选"
			case "strong_buy":
				return "强买入候选"
			}
			return "待评估"
		},
		"tone": func(s string) string {
			switch s {
			case "avoid", "risk_off":
				return "risk"
			case "hold", "bear":
				return "watch"
			case "buy", "strong_buy", "bull":
				return "positive"
			}
			return "neutral"
		},
		"quoteStatus": func(s string) string {
			switch s {
			case "intraday":
				return "盘中"
			case "closed":
				return "收盘"
			case "previous_session":
				return "最近交易日"
			case "unavailable":
				return "缺失"
			}
			return s
		},
		"trend": func(s string) string {
			switch s {
			case "risk_off":
				return "风险回避"
			case "bear":
				return "弱势"
			case "bull":
				return "强势"
			case "neutral_up":
				return "中性偏强"
			case "neutral":
				return "中性"
			case "up":
				return "向上"
			case "down":
				return "向下"
			case "flat":
				return "震荡"
			}
			return "数据不足"
		},
		"reviewScore": func(r types.HoldReview) string {
			if r.Rank <= 0 && !r.InTop {
				return "—"
			}
			return fmt.Sprintf("%.2f", r.Score)
		},
		"rank": func(r types.HoldReview) string {
			if r.Rank <= 0 {
				return "未覆盖"
			}
			return fmt.Sprintf("第%d名", r.Rank)
		},
	}
	t, err := template.New("report").Funcs(funcs).Parse(reportHTML)
	if err != nil {
		return "", fmt.Errorf("parse HTML report: %w", err)
	}
	var b bytes.Buffer
	if err = t.Execute(&b, v); err != nil {
		return "", fmt.Errorf("render HTML report: %w", err)
	}
	return b.String(), nil
}

const reportHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · {{.AsOf}}</title>
<style>
:root{color-scheme:light;--ink:#182c32;--muted:#64777c;--line:#dfe7e5;--paper:#f4f6f2;--green:#176e57;--red:#a63e35;--amber:#8c651a}
*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font:15px/1.65 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif}a{color:var(--green)}
.page{max-width:1200px;margin:auto;padding:40px 28px 60px}.top{display:flex;justify-content:space-between;gap:20px;align-items:center;border-bottom:1px solid var(--line);padding-bottom:24px}.eyebrow{font-size:11px;letter-spacing:2px;color:var(--green);font-weight:700}.top h1{font-size:28px;margin:5px 0 0;letter-spacing:-1px}.meta{font-size:12px;color:var(--muted)}.links{display:flex;gap:16px;font-size:12px;white-space:nowrap}
.hero{display:grid;grid-template-columns:1.8fr 1fr;gap:24px;margin-top:24px}.card{background:#fff;border:1px solid var(--line);border-radius:16px;padding:26px;min-width:0}.decision{background:#183b36;color:#fff;position:relative;overflow:hidden}.decision .meta{color:#b4cec6}.decision h2{font-size:32px;line-height:1.3;margin:18px 0 12px}.decision p{max-width:650px}.pill{display:inline-block;border:1px solid currentColor;border-radius:20px;padding:3px 10px;font-size:12px}.decision .pill{color:#c8e8a6}.statrow{display:flex;gap:28px;border-top:1px solid #3d5b52;margin-top:24px;padding-top:18px}.big{font-size:35px;font-weight:650;line-height:1.25}.statrow .meta{display:block}.subscore{font-size:13px;color:#c7dbd2}.risk{color:var(--red)}.watch{color:var(--amber)}.positive{color:var(--green)}.neutral{color:var(--muted)}
.notes{list-style:none;margin:12px 0 0;padding:0}.notes li{padding:12px 0;border-top:1px solid var(--line);font-size:14px}.notes li:first-child{border-top:0}.notice{margin:20px 0 0;padding:12px 16px;border:1px solid #eadca9;background:#fffae9;border-radius:10px;font-size:13px;color:#735a21}.section-title{display:flex;justify-content:space-between;align-items:baseline;margin:30px 0 12px}.section-title h2{font-size:19px;margin:0}.section-title span{font-size:12px;color:var(--muted)}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}.card h3{font-size:16px;margin:0 0 14px}.reading{white-space:pre-wrap;overflow-wrap:anywhere;font-size:14px}.small{font-size:12px;color:var(--muted)}.table-wrap{overflow-x:auto}table{border-collapse:collapse;width:100%;font-size:13px;text-align:left}th{font-size:11px;color:var(--muted);font-weight:550;white-space:nowrap;padding:10px 12px;border-bottom:1px solid var(--line)}td{padding:13px 12px;border-bottom:1px solid #edf1ee;vertical-align:top}tr:last-child td{border:0}.number{font-variant-numeric:tabular-nums;white-space:nowrap}.fund{font-weight:600}.code{display:block;font-size:11px;color:var(--muted)}.bar{display:inline-block;width:65px;height:5px;background:#e8efea;border-radius:3px;margin-left:8px;vertical-align:middle}.bar i{display:block;height:100%;background:#468d6e;border-radius:3px}.total td{font-weight:700;background:#f3f7f2}.metrics{display:grid;grid-template-columns:1fr 1fr;gap:12px}.metric{padding:14px;background:#f4f7f3;border-radius:8px}.metric strong{font-size:20px;display:block}.metric .small{font-size:11px}.review{padding:16px 0;border-top:1px solid var(--line)}.review:first-child{border-top:0;padding-top:0}.review p{margin:6px 0 0;font-size:13px}.review-head{display:flex;justify-content:space-between;gap:10px}.review-head strong{font-size:14px}.review .pill{font-size:11px}.tag{font-size:11px;padding:2px 7px;background:#eef3ec;border-radius:4px;display:inline-block;margin-right:5px}details{border-top:1px solid var(--line);padding-top:15px;margin-top:16px}summary{cursor:pointer;font-size:13px;font-weight:600}.prices{display:flex;gap:30px;margin-top:16px;flex-wrap:wrap}.prices strong{font-size:20px;display:block}.full{grid-column:1/-1}.footer{border-top:1px solid var(--line);margin-top:30px;padding-top:18px;font-size:11px;color:var(--muted)}
@media(max-width:760px){.page{padding:22px 14px 35px}.top{align-items:flex-start;flex-direction:column;gap:12px}.top h1{font-size:24px}.hero,.grid{grid-template-columns:1fr}.card{padding:20px}.decision h2{font-size:28px}.statrow{gap:22px}.big{font-size:30px}.section-title{align-items:flex-start;gap:10px}.section-title span{max-width:45%;text-align:right}.bar{display:none}td,th{padding:10px 8px}}
@media print{body{background:#fff}.page{max-width:none;padding:0}.links{display:none}.card{break-inside:avoid}.decision{background:#fff;color:var(--ink);border:2px solid var(--ink)}.decision .meta,.subscore,.decision .pill{color:var(--muted)}.grid{display:block}.grid>.card{margin-bottom:15px}details>*{display:block}summary{list-style:none}}
</style></head><body><main class="page">
<header class="top"><div><div class="eyebrow">ETF STRATEGY / DAILY BRIEF</div><h1>{{.Title}}</h1><div class="meta">行情基准日 {{if .AsOf}}{{.AsOf}}{{else}}待确认{{end}} · 生成于 {{.Generated}}</div></div><nav class="links"><a href="{{.BaseName}}.md" download>Markdown</a><a href="{{.BaseName}}.json" download>结构化数据</a></nav></header>
{{if eq .State.RunMode "close-preview"}}<div class="notice">尾盘预估 · {{if .DataTime}}行情最早时间 {{.DataTime}}。{{end}}当天日K尚未收盘，实时价覆盖临时收盘价，成交量按中性处理。此页不是正式收盘确认。</div>{{end}}
<section class="hero"><div class="card decision"><span class="pill">{{if eq .Final.Recommendation "avoid"}}先看风险，再看机会{{else}}规则结论 · 执行前复核{{end}}</span><h2>{{label .Final.Recommendation}}</h2><p>{{.Action}}</p><div class="statrow"><div><span class="meta">综合加权分</span><div class="big">{{num .Final.OverallScore}}<span style="font-size:14px"> / 100</span></div></div><div><span class="meta">策略仓位上限</span><div class="big">{{with .State.Regime}}{{pct .PositionCap}}{{else}}—{{end}}</div></div></div><p class="subscore">基础建议：{{label .Final.BaseRecommendation}} → 风险过滤后：{{label .Final.Recommendation}}</p></div>
<aside class="card"><h3>为什么是这个结论</h3><ul class="notes">{{range .Final.DecisionNotes}}<li>{{.}}</li>{{else}}<li>暂无额外风险过滤说明；请同时查看市场状态和候选数据完整性。</li>{{end}}</ul><p class="small">综合分描述标的强弱，最终动作还受宏观风险、溢价和回撤冷却限制。策略仓位上限针对本策略资金。</p></aside></section>
<div class="section-title"><h2>评分与市场状态</h2><span>评分与明细使用同一计算口径</span></div><section class="grid"><div class="card"><h3>六项因子贡献</h3><div class="table-wrap"><table><thead><tr><th>维度</th><th>子分</th><th>权重</th><th>相对调整</th><th>贡献</th></tr></thead><tbody>{{range .Factors}}<tr><td>{{.Name}}</td><td class="number">{{num .Score}}</td><td>{{pct .Weight}}</td><td>{{adj .Adjustment}}</td><td class="number">{{num .Part}}<span class="bar"><i style="width:{{bar .Score}}%"></i></span></td></tr>{{end}}<tr class="total"><td colspan="4">综合加权分</td><td>{{num .Final.OverallScore}}</td></tr></tbody></table></div></div>
<div class="card"><h3>宏观风险过滤</h3>{{with .State.Regime}}<p><span class="pill {{tone .Trend}}">{{trend .Trend}}</span> <span class="small">基准 {{.Benchmark}}</span></p><div class="metrics"><div class="metric"><span class="small">价格相对20日线</span><strong>{{signedPct .PriceVsMA20}}</strong></div><div class="metric"><span class="small">价格相对60日线</span><strong>{{signedPct .PriceVsMA60}}</strong></div><div class="metric"><span class="small">价格相对120日线</span><strong>{{signedPct .PriceVsMA120}}</strong></div><div class="metric"><span class="small">近60日最大回撤</span><strong>{{pct .DrawDown60}}</strong></div></div><p class="small">价格低于120日线且近60日最大回撤达到8%，触发风险回避。窗口内最大回撤不会因单日反弹立即消失。</p>{{else}}<p class="small">宏观数据未提供，不能将其解释为风险已经解除。</p>{{end}}</div></section>
<div class="section-title"><h2>当前持仓复核</h2><span>缺数据不等于0分，也不等于趋势失效</span></div><section class="grid"><div class="card"><h3>逐项评审</h3>{{range .Final.HoldReviews}}<article class="review"><div class="review-head"><strong>{{.ETFName}} <span class="small">{{.ETFCode}}</span></strong><span class="pill">{{.ActionDesc}}</span></div><div class="small">{{rank .}} · 量化分 {{reviewScore .}} · 技术趋势 {{trend .TechTrend}}</div><p>{{.Rationale}}</p></article>{{else}}<p class="small">本次未提供可评审的持仓数据。</p>{{end}}</div><div class="card"><h3>与策略候选的对照</h3>{{range .Holds}}<article class="review"><strong>{{.CurrentHold}}</strong><p>{{.Suggestion}}</p></article>{{else}}<p class="small">未提供当前持仓对照。持仓由本次输入决定，请核对代码及数量是否仍有效。</p>{{end}}</div></section>
<div class="section-title"><h2>候选观察池</h2><span>候选排名不等同于允许买入</span></div><section class="card"><div class="table-wrap"><table><thead><tr><th>标的 / 代码</th><th>板块</th><th>参考价格</th><th>量化分</th><th>动量状态</th><th>溢价</th></tr></thead><tbody>{{with .State.Screener}}{{range .Top5}}<tr><td class="fund">{{.ETF.Name}}<span class="code">{{.ETF.Code}}{{if .IsCurrentHold}} · 当前持仓{{end}}</span></td><td>{{.ETF.Sector}}</td><td class="number">{{price .ETF.Price}}</td><td>{{num .Score}}</td><td>{{.ActionDesc}}</td><td>{{if gt .ETF.IOPV 0.0}}{{signedPct .ETF.PremiumPct}}{{else}}—{{end}}</td></tr>{{end}}{{end}}</tbody></table></div>
<details><summary>目标标的参考价位{{if eq .Final.Recommendation "avoid"}} · 当前不执行买入{{end}}</summary><p class="small">{{.Final.TargetETF.ETF.Name}} {{.Final.TargetETF.ETF.Code}}。最终建议为回避或观察时，以下数值不构成有效买入计划。</p><div class="prices"><div><span class="small">入场参考</span><strong>{{price .Final.EntryPrice}}</strong></div><div><span class="small">止损参考</span><strong>{{price .Final.StopLoss}}</strong></div><div><span class="small">止盈参考</span><strong>{{price .Final.TakeProfit}}</strong></div></div></details></section>
<div class="section-title"><h2>分析依据</h2><span>执行以风险过滤后的结论为准</span></div><section class="grid"><div class="card full"><h3>综合研判</h3><div class="reading">{{.Final.Reasoning}}</div></div>{{with .State.Tech}}<div class="card"><h3>技术面 <span class="tag">{{trend .Trend}}</span></h3><div class="reading">{{.Summary}}</div></div>{{end}}{{with .State.News}}<div class="card"><h3>消息面</h3>{{range .Highlight}}<p class="small">{{.}}</p>{{end}}<div class="reading">{{.Summary}}</div></div>{{end}}{{with .State.Global}}<div class="card"><h3>海外联动</h3><div class="reading">{{.Summary}}</div>{{if .Snapshots}}<div class="table-wrap"><table><thead><tr><th>指数</th><th>最新</th><th>涨跌幅</th><th>交易日 / 来源</th><th>状态 / 报价时间</th></tr></thead><tbody>{{range .Snapshots}}<tr><td>{{.Index}}</td><td>{{price .Last}}</td><td>{{if eq .Status "unavailable"}}—{{else}}{{num .ChangePc}}%{{end}}</td><td>{{.TradingDate}}<span class="code">{{.Source}}</span></td><td>{{quoteStatus .Status}}<span class="code">{{.AsOf}}</span></td></tr>{{end}}</tbody></table></div>{{end}}</div>{{end}}{{with .State.MoneyFlow}}<div class="card"><h3>资金面</h3><div class="reading">{{.Summary}}</div><p class="small">资金流为量价代理估算，不是真实北向或申赎数据。</p></div>{{end}}</section>
<footer class="footer">本报告由规则与多维分析生成，仅供研究参考。行情与净值可能存在时差；执行前核对数据日期、费用及真实持仓。<br>完整 Markdown 和 JSON 与本页同目录保存。</footer>
</main></body></html>`
