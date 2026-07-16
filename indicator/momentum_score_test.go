package indicator

import (
	"math"
	"testing"
	"time"

	"github.com/multi-agents-etf-trade-strategy/types"
)

func mkKlines(closes []float64) []types.KLine {
	out := make([]types.KLine, len(closes))
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, c := range closes {
		out[i] = types.KLine{
			Date: base.AddDate(0, 0, i),
			Open: c, Close: c, High: c, Low: c, Volume: 1000,
		}
	}
	return out
}

func TestMomentumScore_Uptrend(t *testing.T) {
	closes := make([]float64, 21)
	for i := range closes {
		// 每日 +1% 复合上涨
		closes[i] = math.Pow(1.01, float64(i))
	}
	score, ann, r2 := MomentumScore(mkKlines(closes), 21)
	if score <= 0 {
		t.Fatalf("expect positive score for uptrend, got %v", score)
	}
	if ann <= 0 {
		t.Fatalf("expect positive annualized, got %v", ann)
	}
	if r2 < 0.99 {
		t.Fatalf("strict log-linear series should have R²≈1, got %v", r2)
	}
}

func TestMomentumScore_Downtrend(t *testing.T) {
	closes := make([]float64, 21)
	for i := range closes {
		closes[i] = math.Pow(0.99, float64(i))
	}
	score, ann, _ := MomentumScore(mkKlines(closes), 21)
	if score >= 0 {
		t.Fatalf("expect negative score for downtrend, got %v", score)
	}
	if ann >= 0 {
		t.Fatalf("expect negative annualized, got %v", ann)
	}
}

func TestMomentumScore_NotEnoughData(t *testing.T) {
	score, ann, r2 := MomentumScore(mkKlines([]float64{1, 2, 3}), 21)
	if score != 0 || ann != 0 || r2 != 0 {
		t.Fatalf("expect zeros when not enough data, got %v %v %v", score, ann, r2)
	}
}

func TestMomentumScore_NonPositiveClose(t *testing.T) {
	closes := []float64{1, 2, 0, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21}
	score, _, _ := MomentumScore(mkKlines(closes), 21)
	if score != 0 {
		t.Fatalf("expect 0 when any close <= 0, got %v", score)
	}
}

func TestMomentumScore_BadParam(t *testing.T) {
	score, _, _ := MomentumScore(mkKlines([]float64{1, 2, 3}), 1)
	if score != 0 {
		t.Fatalf("mDays<=1 should return 0, got %v", score)
	}
}

func TestMomentumScore_MatchesJoinQuantPolyfitWeights(t *testing.T) {
	// 期望值由聚宽原版等价的 numpy.polyfit(x, log(prices), 1, w=linspace(1,2,21))
	// 计算；该非完全线性序列可以区分 w 与 w² 两种回归口径。
	closes := []float64{
		1.00, 1.02, 1.01, 1.04, 1.03, 1.07, 1.06,
		1.10, 1.09, 1.13, 1.12, 1.16, 1.15, 1.20,
		1.18, 1.23, 1.22, 1.27, 1.25, 1.31, 1.29,
	}
	score, ann, r2 := MomentumScore(mkKlines(closes), 21)
	assertNear := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-10 {
			t.Fatalf("%s=%0.15f, want %0.15f", name, got, want)
		}
	}
	assertNear("score", score, 29.14087848599015)
	assertNear("annualized", ann, 29.862151209067203)
	assertNear("r2", r2, 0.9758465919609285)
}
