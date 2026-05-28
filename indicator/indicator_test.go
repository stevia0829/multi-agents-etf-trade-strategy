package indicator

import (
	"testing"
)

func TestMA(t *testing.T) {
	k := mkKlines([]float64{1, 2, 3, 4, 5})
	if got := MA(k, 5); got != 3 {
		t.Fatalf("MA expect 3, got %v", got)
	}
	if got := MA(k, 10); got != 0 {
		t.Fatalf("MA insufficient should be 0, got %v", got)
	}
	if got := MA(k, 0); got != 0 {
		t.Fatalf("MA n<=0 should be 0, got %v", got)
	}
}

func TestMomentum_NEdge(t *testing.T) {
	k := mkKlines([]float64{1, 2, 3, 4, 5})
	if got := Momentum(k, 0); got != 0 {
		t.Fatalf("Momentum n<=0 should be 0, got %v", got)
	}
	if got := Momentum(k, 100); got != 0 {
		t.Fatalf("insufficient should be 0, got %v", got)
	}
	got := Momentum(k, 4) // (5-1)/1 = 4
	if got != 4 {
		t.Fatalf("expect 4, got %v", got)
	}
}

func TestVolatility(t *testing.T) {
	k := mkKlines([]float64{10, 10, 10, 10, 10})
	if got := Volatility(k, 4); got != 0 {
		t.Fatalf("flat series volatility should be 0, got %v", got)
	}
}
