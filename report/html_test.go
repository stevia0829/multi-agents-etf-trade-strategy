package report

import (
	"strings"
	"testing"
	"time"

	"github.com/multi-agents-etf-trade-strategy/types"
)

func TestHTMLReportEscapesExternalTextAndDisclosesPartialData(t *testing.T) {
	st := mkState()
	st.RunMode = "close-preview"
	st.Final.Recommendation = "avoid"
	st.Final.BaseRecommendation = "strong_buy"
	st.Final.DecisionNotes = []string{"风险否决 <script>alert(1)</script>"}
	st.Final.Reasoning = "<img src=x onerror=alert(1)>"
	st.Final.HoldReviews = []types.HoldReview{{ETFCode: "999999", Action: "observe", ActionDesc: "数据不足，暂不判定", Rationale: "需要补数据"}}
	st.HoldAdvices = []types.HoldAdvice{{CurrentHold: "999999", Suggestion: "建议轮动至危险标的"}}
	got, err := BuildHTML(st, time.Date(2026, 11, 2, 14, 50, 0, 0, time.FixedZone("CST", 8*3600)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<!doctype html>", "当天日K尚未收盘", "当前不执行买入", "回避 · 暂停新增", "&lt;script&gt;", "&lt;img", "数据不足，暂不判定", "etf-report-20261102-145000.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"<script>alert(1)</script>", "<img src=x", "建议轮动至危险标的", "ZgotmplZ", "+7750.0%"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe or inconsistent output %q", bad)
		}
	}
}

func TestMarkdownAdjustmentAndHoldAdviceDoNotMislead(t *testing.T) {
	st := mkState()
	st.Final.Recommendation = "avoid"
	st.Final.ScoreBreakdown["news_adj"] = 1.1
	st.HoldAdvices = []types.HoldAdvice{{CurrentHold: "999999", Suggestion: "建议轮动至危险标的"}}
	got := BuildMarkdown(st, time.Now())
	if strings.Contains(got, "+7750.0%") || strings.Contains(got, "+6400.0%") || strings.Contains(got, "建议轮动至危险标的") {
		t.Fatal("misleading adjustment or rotation advice")
	}
	if !strings.Contains(got, "+10.0%") || !strings.Contains(got, "暂停新开仓与轮动买入") {
		t.Fatal("real adjustment or risk disclosure missing")
	}
}

func TestHTMLReportHandlesMissingAnalysis(t *testing.T) {
	got, err := BuildHTML(nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "数据尚不完整") {
		t.Fatal("missing-data state not disclosed")
	}
}
