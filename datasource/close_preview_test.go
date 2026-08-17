package datasource

import (
	"errors"
	"testing"
	"time"

	"github.com/multi-agents-etf-trade-strategy/types"
)

type closePreviewStub struct {
	klines []types.KLine
	quote  RealtimeQuote
}

func (s *closePreviewStub) ListAllETFs() ([]types.ETF, error) { return nil, nil }
func (s *closePreviewStub) GetKLine(string, int) ([]types.KLine, error) {
	return s.klines, nil
}
func (s *closePreviewStub) GetKLineAsOf(string, int, time.Time) ([]types.KLine, error) {
	return s.klines, nil
}
func (s *closePreviewStub) FetchRealtimeQuote(string) (RealtimeQuote, error) {
	if s.quote.Price <= 0 {
		return RealtimeQuote{}, errors.New("no quote")
	}
	return s.quote, nil
}

func TestClosePreviewUsesRealtimePriceInTodayKLine(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 50, 30, 0, time.Local)
	stub := &closePreviewStub{
		klines: []types.KLine{{
			Date: time.Date(2026, 8, 18, 0, 0, 0, 0, time.Local),
			Open: 4.70, High: 4.80, Low: 4.68, Close: 4.80, Volume: 100,
		}},
		quote: RealtimeQuote{Price: 4.801, Time: now.Add(-10 * time.Second)},
	}
	ds, err := NewClosePreviewDataSource(stub)
	if err != nil {
		t.Fatal(err)
	}
	ds.now = func() time.Time { return now }

	got, err := ds.GetKLine("510300", 21)
	if err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if last.Close != 4.801 || !last.IsPartial {
		t.Fatalf("last=%+v, want realtime close and partial marker", last)
	}
	if stub.klines[0].IsPartial {
		t.Fatal("wrapper must not mutate datasource-owned kline")
	}
}

func TestClosePreviewRejectsPreviousDayKLine(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 50, 30, 0, time.Local)
	stub := &closePreviewStub{
		klines: []types.KLine{{Date: now.AddDate(0, 0, -1), Close: 4.80}},
		quote:  RealtimeQuote{Price: 4.801, Time: now.Add(-10 * time.Second)},
	}
	ds, _ := NewClosePreviewDataSource(stub)
	ds.now = func() time.Time { return now }

	if _, err := ds.GetKLine("510300", 21); err == nil {
		t.Fatal("expected previous-day kline to be rejected")
	}
}

func TestClosePreviewRejectsStaleQuote(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 50, 30, 0, time.Local)
	stub := &closePreviewStub{
		klines: []types.KLine{{Date: now, Close: 4.80}},
		quote:  RealtimeQuote{Price: 4.801, Time: now.Add(-3 * time.Minute)},
	}
	ds, _ := NewClosePreviewDataSource(stub)
	ds.now = func() time.Time { return now }

	if _, err := ds.GetKLine("510300", 21); err == nil {
		t.Fatal("expected stale quote to be rejected")
	}
}

func TestClosePreviewRejectsSnapshotThatExpiredDuringPipeline(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 50, 30, 0, time.Local)
	stub := &closePreviewStub{
		klines: []types.KLine{{Date: now, Close: 4.80}},
		quote:  RealtimeQuote{Price: 4.801, Time: now.Add(-10 * time.Second)},
	}
	ds, _ := NewClosePreviewDataSource(stub)
	ds.now = func() time.Time { return now }
	if _, err := ds.GetKLine("510300", 21); err != nil {
		t.Fatal(err)
	}
	if err := ds.ValidateSnapshot(now.Add(7 * time.Minute)); err == nil {
		t.Fatal("expected snapshot to expire while pipeline is running")
	}
}
