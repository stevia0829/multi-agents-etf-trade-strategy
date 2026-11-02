package backtest

import "testing"

func TestNewEngineUsesExecutableBacktestDefaults(t *testing.T) {
	eng := NewEngine(nil)
	if !eng.EntryNextOpen {
		t.Fatal("EntryNextOpen must default to true to avoid same-close lookahead")
	}
	if eng.MinHistoryBars != 60 {
		t.Fatalf("MinHistoryBars=%d, want 60", eng.MinHistoryBars)
	}
}
