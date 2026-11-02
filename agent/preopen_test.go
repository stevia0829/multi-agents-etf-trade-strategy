package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/multi-agents-etf-trade-strategy/datasource"
	"github.com/multi-agents-etf-trade-strategy/types"
)

// preOpenMockDS 在 mockDS 基础上补 RealtimeQuoter，用于 PreOpenAgent 的集合竞价撮合桩。
type preOpenMockDS struct {
	*mockDS
	quotes map[string]datasource.RealtimeQuote
}

func (m *preOpenMockDS) FetchRealtimeQuote(code string) (datasource.RealtimeQuote, error) {
	if q, ok := m.quotes[code]; ok {
		return q, nil
	}
	return datasource.RealtimeQuote{}, errNoQuote(code)
}

type quoteErr string

func (e quoteErr) Error() string { return "no quote for " + string(e) }
func errNoQuote(code string) error { return quoteErr(code) }

func newSnap(code string, auction, gap float64) types.PreOpenSnapshot {
	return types.PreOpenSnapshot{
		ETFCode:      code,
		ETFName:      code,
		PrevClose:    1.0,
		AuctionPrice: auction,
		GapPct:       gap,
	}
}

func absDiff(a, b float64) float64 {
	if a < b {
		return b - a
	}
	return a - b
}

func TestApplyReduceRule_TrimHighOpen(t *testing.T) {
	s := newSnap("515880", 1.020, 0.02)
	applyReduceRule(&s, types.HoldReview{ETFCode: "515880", Action: "trim"})
	if s.Verdict != "reduce" || s.SellRatio != 0.5 || s.SellType != "open_market" {
		t.Fatalf("trim高开: verdict=%s ratio=%v type=%s, want reduce/0.5/open_market", s.Verdict, s.SellRatio, s.SellType)
	}
	if s.AdjSellPrice != 1.020 {
		t.Fatalf("trim高开 adj_sell_price=%v, want 1.020", s.AdjSellPrice)
	}
	if s.AdjEntry != 0 {
		t.Fatalf("reduce 必须不带买入价, adj_entry=%v", s.AdjEntry)
	}
}

func TestApplyReduceRule_TrimLowOpen(t *testing.T) {
	s := newSnap("515880", 0.990, -0.01)
	applyReduceRule(&s, types.HoldReview{ETFCode: "515880", Action: "trim"})
	if s.SellType != "limit" {
		t.Fatalf("trim低开 sell_type=%s, want limit", s.SellType)
	}
	want := 0.990 * 1.003
	if absDiff(s.AdjSellPrice, want) > 1e-9 {
		t.Fatalf("trim低开 adj_sell_price=%v, want %v", s.AdjSellPrice, want)
	}
	if s.SellRatio != 0.5 {
		t.Fatalf("trim sell_ratio=%v, want 0.5", s.SellRatio)
	}
}

func TestApplyReduceRule_Rotate(t *testing.T) {
	s := newSnap("515880", 1.020, 0.02)
	applyReduceRule(&s, types.HoldReview{ETFCode: "515880", Action: "rotate"})
	if s.SellRatio != 1.0 || s.SellType != "open_market" {
		t.Fatalf("rotate ratio=%v type=%s, want 1.0/open_market", s.SellRatio, s.SellType)
	}
	if s.AdjSellPrice != 1.020 {
		t.Fatalf("rotate adj_sell_price=%v, want 1.020", s.AdjSellPrice)
	}
}

func TestApplyReduceRule_AuctionMissing(t *testing.T) {
	s := newSnap("515880", 0, -0.01) // 撮合缺失
	applyReduceRule(&s, types.HoldReview{ETFCode: "515880", Action: "trim"})
	if s.SellType != "open_market" || s.AdjSellPrice != 0 {
		t.Fatalf("撮合缺失: type=%s price=%v, want open_market/0", s.SellType, s.AdjSellPrice)
	}
	if !strings.Contains(s.Note, "撮合缺失") {
		t.Fatalf("撮合缺失 note 应提示市价减仓, got %q", s.Note)
	}
}

func TestPreOpenRun_HoldTrimTriggersReduce(t *testing.T) {
	// 515880 是被 HoldReview 标记 trim 的持仓，但当天 TargetETF 是 588080、Picks 为空。
	// 验证：候选集扩展把 515880 纳入复核，并产出 reduce；588080 走正常买侧。
	state := &types.AgentState{
		CurrentHolds: []string{"515880"},
		Final: &types.FinalDecision{
			TargetETF:  types.ScoredETF{ETF: types.ETF{Code: "588080", Name: "科创新材料"}},
			EntryPrice: 1.000, StopLoss: 0.970, TakeProfit: 1.060,
			Picks: nil,
			HoldReviews: []types.HoldReview{
				{ETFCode: "515880", ETFName: "通信ETF", Action: "trim", ActionDesc: "减仓观察"},
			},
		},
	}
	ds := &preOpenMockDS{
		mockDS: &mockDS{},
		quotes: map[string]datasource.RealtimeQuote{
			"510300": {Code: "510300", Name: "沪深300ETF", Price: 1.000, PrevClose: 1.000, IOPV: 1.000},
			"588080": {Code: "588080", Name: "科创新材料", Price: 1.000, PrevClose: 1.000, IOPV: 1.000},
			"515880": {Code: "515880", Name: "通信ETF", Price: 1.015, PrevClose: 1.000, IOPV: 1.000},
		},
	}
	a := NewPreOpenAgent(nil, ds)
	res, err := a.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("preopen run: %v", err)
	}

	var comm *types.PreOpenSnapshot
	var tgt *types.PreOpenSnapshot
	for i := range res.Snapshots {
		switch res.Snapshots[i].ETFCode {
		case "515880":
			comm = &res.Snapshots[i]
		case "588080":
			tgt = &res.Snapshots[i]
		}
	}
	if comm == nil {
		t.Fatalf("515880 应被纳入 snapshots（候选集扩展），实际 snapshots=%+v", res.Snapshots)
	}
	if comm.Verdict != "reduce" || comm.HoldAction != "trim" || comm.SellRatio != 0.5 {
		t.Fatalf("515880 reduce 断言失败: verdict=%s hold=%s ratio=%v", comm.Verdict, comm.HoldAction, comm.SellRatio)
	}
	if comm.SellType != "open_market" || comm.AdjSellPrice != 1.015 {
		t.Fatalf("515880 高开应开盘卖: type=%s price=%v", comm.SellType, comm.AdjSellPrice)
	}
	if tgt == nil || tgt.Verdict == "reduce" {
		t.Fatalf("588080 应走买侧(非reduce), got %+v", tgt)
	}
	if !strings.Contains(res.FinalAction, "减仓") || !strings.Contains(res.FinalAction, "515880") {
		t.Fatalf("FinalAction 应含减仓515880, got %q", res.FinalAction)
	}
}

func TestPreOpenRun_NoHoldReviewsUnchanged(t *testing.T) {
	// 回归：HoldReviews 为空时，行为与改动前一致（纯买侧，无 reduce）。
	state := &types.AgentState{
		Final: &types.FinalDecision{
			TargetETF:  types.ScoredETF{ETF: types.ETF{Code: "588080", Name: "科创新材料"}},
			EntryPrice: 1.000, StopLoss: 0.970, TakeProfit: 1.060,
		},
	}
	ds := &preOpenMockDS{
		mockDS: &mockDS{},
		quotes: map[string]datasource.RealtimeQuote{
			"510300": {Code: "510300", Price: 1.000, PrevClose: 1.000, IOPV: 1.000},
			"588080": {Code: "588080", Price: 1.000, PrevClose: 1.000, IOPV: 1.000},
		},
	}
	a := NewPreOpenAgent(nil, ds)
	res, err := a.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("preopen run: %v", err)
	}
	for _, s := range res.Snapshots {
		if s.Verdict == "reduce" {
			t.Fatalf("无 HoldReviews 时不应出现 reduce, got %+v", s)
		}
		if s.SellRatio != 0 || s.SellType != "" {
			t.Fatalf("无 reduce 时卖出字段应为零值, got %+v", s)
		}
	}
}
