package agent

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/multi-agents-etf-trade-strategy/datasource"
	"github.com/multi-agents-etf-trade-strategy/llm"
	"github.com/multi-agents-etf-trade-strategy/types"
)

type decisionTestLLM struct {
	reply string
	err   error
}

func (c decisionTestLLM) Name() string { return "offline-test" }
func (c decisionTestLLM) Chat(context.Context, string, string, ...llm.ChatOptions) (string, error) {
	return c.reply, c.err
}

func decisionTestState(trend string) *types.AgentState {
	top := []types.ScoredETF{
		{ETF: types.ETF{Code: "513100", Name: "纳指ETF", Sector: "海外", Price: 2}, Score: 75, Indicators: map[string]float64{"Strategy3Score": .5}, Action: string(ActionBuy)},
		{ETF: types.ETF{Code: "512800", Name: "银行ETF", Sector: "金融", Price: 1}, Score: 80, Action: string(ActionBuy)},
	}
	r := &types.RegimeAnalysis{Benchmark: "510300", Trend: trend, Score: 85, PositionCap: 1}
	if trend == "risk_off" {
		r.Score = 15
		r.PositionCap = 0
		r.DrawDown60 = .15
		r.PriceVsMA120 = -.1
	}
	if trend == "bear" {
		r.Score = 35
		r.PositionCap = .4
	}
	return &types.AgentState{Screener: &types.ScreenerResult{Best: top[0], Top5: top}, Regime: r,
		News: &types.NewsAnalysis{Score: 75, Sentiment: "positive"}, Tech: &types.TechnicalAnalysis{Score: 75, Trend: "up"},
		Global: &types.GlobalMarketAnalysis{Score: 75}, MoneyFlow: &types.MoneyFlowAnalysis{Score: 75}}
}

func TestFinalScoreAndRecommendationAreIndependentOfModelArithmetic(t *testing.T) {
	for _, trend := range []string{"bull", "bear", "risk_off"} {
		t.Run(trend, func(t *testing.T) {
			st := decisionTestState(trend)
			model := decisionTestLLM{reply: `{"overall_score":1,"recommendation":"avoid","reasoning":"标的观察","picks":[{"etf_code":"513100","recommendation":"strong_buy"},{"etf_code":"512800","recommendation":"strong_buy"}]}`}
			got, err := NewFinalAgent(model).Run(context.Background(), st)
			if err != nil {
				t.Fatal(err)
			}
			want := weightedScore(st)
			if got.OverallScore != want {
				t.Fatalf("score=%v want=%v", got.OverallScore, want)
			}
			sum := 0.0
			for _, k := range []string{"quant_part", "tech_part", "news_part", "global_part", "regime_part", "flow_part"} {
				sum += got.ScoreBreakdown[k]
			}
			if math.Abs(sum-got.OverallScore) > 1e-9 {
				t.Fatalf("parts=%v score=%v", sum, got.OverallScore)
			}
			if got.BaseRecommendation != RuleRecommend(want) {
				t.Fatal("base threshold mismatch")
			}
			if got.Recommendation != CapRecommendation(RuleRecommend(want), st.Regime) {
				t.Fatalf("wrong final %s", got.Recommendation)
			}
			if trend == "risk_off" && (len(got.Picks) != 0 || len(got.DecisionNotes) == 0) {
				t.Fatal("risk veto must remove picks and explain reason")
			}
			if trend == "bear" {
				for _, p := range got.Picks {
					if p.Recommendation != "hold" {
						t.Fatalf("bear pick bypassed gate: %+v", p)
					}
				}
			}
			fallback, err := NewFinalAgent(decisionTestLLM{err: errors.New("offline")}).Run(context.Background(), st)
			if err != nil {
				t.Fatal(err)
			}
			if fallback.OverallScore != got.OverallScore || fallback.Recommendation != got.Recommendation {
				t.Fatal("model and offline paths disagree")
			}
			if trend == "risk_off" && len(fallback.Picks) != 0 {
				t.Fatal("offline alternatives bypassed veto")
			}
		})
	}
}

func TestHoldAdviceUsesFinalExecutionPermission(t *testing.T) {
	st := decisionTestState("bull")
	for _, reco := range []string{"avoid", "hold", "buy"} {
		f := &types.FinalDecision{Recommendation: reco}
		got := BuildHoldAdvices([]string{"999999", "513100"}, st.Screener.Top5, f)
		if len(got) != 2 {
			t.Fatal("lost holds")
		}
		if reco == "buy" && !strings.Contains(got[0].Suggestion, "建议轮动至") {
			t.Fatal("normal rotation unexpectedly removed")
		}
		if reco != "buy" {
			for _, a := range got {
				if strings.Contains(a.Suggestion, "建议轮动至") || strings.Contains(a.Suggestion, "可适度加仓") {
					t.Fatalf("blocked advice suggests buying: %s", a.Suggestion)
				}
			}
		}
	}
}

func TestMissingHoldDataCannotBecomeModelSellSignal(t *testing.T) {
	st := decisionTestState("bull")
	st.CurrentHolds = []string{"999999"}
	got := sanitizeHoldReviews([]types.HoldReview{{ETFCode: "999999", Action: "rotate", Rationale: "量化0分，建议清仓"}}, st)
	if len(got) != 1 || got[0].Action != "observe" || got[0].TechTrend != "unknown" || strings.Contains(got[0].Rationale, "建议清仓") {
		t.Fatalf("missing-data sell advice survived: %+v", got)
	}
}

func TestPreOpenCannotReopenRiskVetoAfterModelEnrichment(t *testing.T) {
	st := decisionTestState("risk_off")
	st.Final = &types.FinalDecision{TargetETF: st.Screener.Best, Recommendation: "avoid", EntryPrice: 2, StopLoss: 1.9, TakeProfit: 2.2}
	ds := &preOpenMockDS{mockDS: &mockDS{}, quotes: map[string]datasource.RealtimeQuote{
		"510300": {Price: 4, PrevClose: 4, IOPV: 4}, "513100": {Price: 2, PrevClose: 2, IOPV: 2},
	}}
	model := decisionTestLLM{reply: `{"snapshots":[{"etf_code":"513100","verdict":"chase","adj_entry":2}],"summary":"立即买入","final_action":"建议追入513100"}`}
	got, err := NewPreOpenAgent(model, ds).Run(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Snapshots) != 1 || got.Snapshots[0].Verdict != "abandon" || got.Snapshots[0].AdjEntry != 0 || strings.Contains(got.FinalAction, "追入") {
		t.Fatalf("preopen bypassed veto: %+v", got)
	}
}

func TestPreOpenDecisionLimitPreservesSellsAndAllowsBullBuys(t *testing.T) {
	for _, reco := range []string{"hold", "buy"} {
		st := decisionTestState("bull")
		st.Final = &types.FinalDecision{Recommendation: reco}
		st.CurrentHolds = []string{"515880"}
		out := &types.PreOpenAnalysis{Snapshots: []types.PreOpenSnapshot{{Verdict: "chase", AdjEntry: 2}, {ETFCode: "515880", HoldAction: "trim", Verdict: "chase", SellRatio: 1, AuctionPrice: 1}}}
		applyPreOpenDecisionLimit(out, st)
		if out.Snapshots[1].Verdict != "reduce" || out.Snapshots[1].SellRatio != .5 {
			t.Fatal("existing sell changed")
		}
		if reco == "hold" && (out.Snapshots[0].AdjEntry != 0 || out.Snapshots[0].Verdict != "wait_pullback") {
			t.Fatal("hold allows buy")
		}
		if reco == "buy" && out.Snapshots[0].Verdict != "chase" {
			t.Fatal("bull buy unexpectedly blocked")
		}
	}
}
