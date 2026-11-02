package agent

import (
	"math"
	"testing"
)

// TestClampStopTake 锁定 advice 价位合法区间（AGENTS.md §6），拦截 LLM 越界值。
func TestClampStopTake(t *testing.T) {
	const price = 1.000
	tests := []struct {
		name                     string
		stopIn, takeIn           float64
		wantStop, wantTake       float64
	}{
		{"区间内不变", price * 0.95, price * 1.10, price * 0.95, price * 1.10},
		{"止损过高(>0.97)拉回", price * 0.99, price * 1.10, price * 0.97, price * 1.10},
		{"止损过低(<0.88)拉回", price * 0.50, price * 1.10, price * 0.88, price * 1.10}, // 真实风险：LLM 返回跌 50%
		{"止盈过高(>1.20)拉回", price * 0.95, price * 3.00, price * 0.95, price * 1.20}, // 真实风险：LLM 返回翻倍
		{"止盈过低(<1.05)拉回", price * 0.95, price * 1.01, price * 0.95, price * 1.05},
		{"price<=0 原样返回", 0.5, 2.0, 0.5, 2.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := price
			if tt.name == "price<=0 原样返回" {
				p = 0
			}
			stop, take := clampStopTake(p, tt.stopIn, tt.takeIn)
			if math.Abs(stop-tt.wantStop) > 1e-9 || math.Abs(take-tt.wantTake) > 1e-9 {
				t.Errorf("clampStopTake(%g,%g,%g) = (stop=%g,take=%g), want (%g,%g)",
					p, tt.stopIn, tt.takeIn, stop, take, tt.wantStop, tt.wantTake)
			}
			// 区间内时必须保证 stop < price < take
			if p > 0 && !(stop < p && p < take) {
				t.Errorf("invariant stop<price<take broken: stop=%g price=%g take=%g", stop, p, take)
			}
		})
	}
}

// TestIntradayTrend 跨刷新趋势标签：至少 3 点才判，单调=连跌/连涨，否则震荡。
func TestIntradayTrend(t *testing.T) {
	tests := []struct {
		prices []float64
		want   string
	}{
		{[]float64{1.0, 0.95, 0.90}, "近3次连跌"},
		{[]float64{1.0, 1.05, 1.10, 1.15}, "近4次连涨"},
		{[]float64{1.0, 1.05, 1.0, 1.03}, "近4次震荡"},
		{[]float64{1.0, 1.05}, ""}, // 不足 3 点
		{[]float64{1.0}, ""},
	}
	for _, tt := range tests {
		if got := intradayTrend(tt.prices); got != tt.want {
			t.Errorf("intradayTrend(%v) = %q, want %q", tt.prices, got, tt.want)
		}
	}
}
