package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/multi-agents-etf-trade-strategy/datasource"
	"github.com/multi-agents-etf-trade-strategy/types"
)

func TestGlobalSnapshotsCannotHallucinateMissingQuotes(t *testing.T) {
	res := &types.GlobalMarketAnalysis{USPrev: types.MarketSnapshot{Index: "SPX", Change: 999, ChangePc: 9}}
	quotes := map[string]datasource.IndexQuote{"SPX": {Symbol: "SPX", Source: "unavailable", Error: "HTTP 503"}, "NDX": {Symbol: "NDX", Last: 110, PrevClose: 100, Change: 10, ChangePct: .1, TradingDate: "2026-10-30", Time: time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC), Source: "eastmoney", Status: "previous_session"}}
	overrideSnapshotsWithReal(res, quotes)
	if res.USPrev.Change != 0 || res.USPrev.Status != "unavailable" || !strings.Contains(res.USPrev.Note, "HTTP 503") {
		t.Fatalf("model snapshot survived %+v", res.USPrev)
	}
	if len(res.Snapshots) != 6 || res.Snapshots[1].Index != "NDX" || res.Snapshots[1].TradingDate != "2026-10-30" {
		t.Fatal("full metadata missing")
	}
}

func TestCalendarKeepsUSCloseAtEarlyChinaMorning(t *testing.T) {
	anchor, _ := time.Parse(time.RFC3339, "2026-11-03T07:00:00+08:00")
	closed, _ := time.Parse(time.RFC3339, "2026-11-02T16:00:00-05:00")
	quotes := map[string]datasource.IndexQuote{"SPX": {Symbol: "SPX", Last: 110, PrevClose: 100, Time: closed}}
	applyCalendarSemantics(quotes, anchor)
	if quotes["SPX"].Last != 110 {
		t.Fatal("same UTC date incorrectly discarded")
	}
	quotes["N225"] = datasource.IndexQuote{Symbol: "N225", Last: 110, PrevClose: 100, Time: anchor.Add(time.Hour)}
	applyCalendarSemantics(quotes, anchor)
	if quotes["N225"].Last != 0 || quotes["N225"].Error == "" {
		t.Fatal("future intraday data not marked unavailable")
	}
}
