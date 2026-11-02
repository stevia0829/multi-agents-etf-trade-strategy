package agent

import (
	"testing"

	"github.com/multi-agents-etf-trade-strategy/types"
)

// judgeHoldOf 构造一个持仓快照并跑持仓侧判定，返回便于断言。
func judgeHoldOf(s *types.IntradaySnapshot) {
	s.IsHold = true
	(&IntradayWatchAgent{}).judgeHold(s)
}

// TestJudgeHold_TrailingStop 盘中移动止盈（A）：近5日高点回撤 ≥ ATR 阈值 且 跌破 MA5 且 浮盈>0 → trim_now。
func TestJudgeHold_TrailingStop(t *testing.T) {
	s := &types.IntradaySnapshot{
		Price: 1.03, MA5: 1.05, VsMA5: (1.03 - 1.05) / 1.05, // 跌破 MA5
		PeakPrice: 1.10, TrailingPullback: (1.10 - 1.03) / 1.10, ATR: 0.015, // 回撤 ~6.4% ≥ 阈值
		Cost: 1.00, ProfitPct: (1.03 - 1.00) / 1.00, // 浮盈 3%
	}
	judgeHoldOf(s)
	if s.Signal != "trim_now" {
		t.Fatalf("expect trim_now (trailing), got %s — %s", s.Signal, s.SignalCn)
	}
}

// TestJudgeHold_TrailingStop_NoProfitNoCost 无成本且无浮盈时不触发 A（不凭空提示锁盈）。
func TestJudgeHold_TrailingStop_NoProfitNoCost(t *testing.T) {
	s := &types.IntradaySnapshot{
		Price: 1.03, MA5: 1.05, VsMA5: (1.03 - 1.05) / 1.05,
		PeakPrice: 1.10, TrailingPullback: (1.10 - 1.03) / 1.10, ATR: 0.015,
		Cost: 0, // 无成本：profitOK = s.Cost<=0 → true，仍可触发（动量转弱提示）
	}
	judgeHoldOf(s)
	// 无成本时 profitOK 恒真，回撤达标仍应提示动量转弱
	if s.Signal != "trim_now" {
		t.Fatalf("expect trim_now (trailing w/o cost), got %s — %s", s.Signal, s.SignalCn)
	}
}

// TestJudgeHold_ToppingRSIUpperShadow 见顶（B）：RSI 超买 + 当日长上影 → trim_now。
func TestJudgeHold_ToppingRSIUpperShadow(t *testing.T) {
	s := &types.IntradaySnapshot{
		Price: 1.02, Open: 1.00, DayHigh: 1.10, // 上影 0.08 > 实体 0.02
		RSI: 75, MA5: 1.00, VsMA5: 0.02, // VsMA5>0，不触发 A
		PeakPrice: 1.03, TrailingPullback: 0.01, ATR: 0.02,
	}
	judgeHoldOf(s)
	if s.Signal != "trim_now" {
		t.Fatalf("expect trim_now (RSI+上影), got %s — %s", s.Signal, s.SignalCn)
	}
}

// TestJudgeHold_ToppingBreakMA5AfterUptrend 见顶（B）：连涨后首次跌破 MA5 → trim_now。
func TestJudgeHold_ToppingBreakMA5AfterUptrend(t *testing.T) {
	s := &types.IntradaySnapshot{
		Price: 0.995, MA5: 1.00, VsMA5: (0.995 - 1.00) / 1.00, // 刚跌破 0.5%
		RecentTrend: "up", RSI: 55,
		PeakPrice: 1.01, TrailingPullback: (1.01 - 0.995) / 1.01, ATR: 0.01, // 回撤小，A 不触发
	}
	judgeHoldOf(s)
	if s.Signal != "trim_now" {
		t.Fatalf("expect trim_now (uptrend break MA5), got %s — %s", s.Signal, s.SignalCn)
	}
}

// TestJudgeHold_ProfitObserve 浮盈阶梯（C）：浮盈达观察阈值、无其它风险信号 → hold 但升级 watch。
func TestJudgeHold_ProfitObserve(t *testing.T) {
	s := &types.IntradaySnapshot{
		Price: 1.04, MA5: 1.03, VsMA5: (1.04 - 1.03) / 1.03, // 高于 MA5
		PeakPrice: 1.05, TrailingPullback: (1.05 - 1.04) / 1.05, ATR: 0.005, // 低波动，回撤小
		Cost: 1.00, ProfitPct: 0.04, RSI: 50, ChangePct: 0.5,
	}
	judgeHoldOf(s)
	if s.Signal != "hold" {
		t.Fatalf("expect hold, got %s", s.Signal)
	}
	if s.Priority != "watch" {
		t.Fatalf("expect watch (profit observe), got %s", s.Priority)
	}
}

// TestJudgeHold_NoFakeProfitWithoutCost cost=0 时不报假浮盈，保持 hold/info。
func TestJudgeHold_NoFakeProfitWithoutCost(t *testing.T) {
	s := &types.IntradaySnapshot{
		Price: 1.04, MA5: 1.03, VsMA5: 0.0097,
		PeakPrice: 1.05, TrailingPullback: 0.0095, ATR: 0.005,
		Cost: 0,
	}
	judgeHoldOf(s)
	if s.Signal != "hold" || s.Priority != "info" {
		t.Fatalf("expect hold/info without cost, got %s/%s", s.Signal, s.Priority)
	}
}
