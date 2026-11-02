package datasource

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type indexTestTransport func(*http.Request) (*http.Response, error)

func (f indexTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func indexResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func indexTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestIndexFetcherUsesCorrectBatchSymbolsAndRealPreviousClose(t *testing.T) {
	for _, object := range []bool{true, false} {
		var calls int32
		f := NewIndexFetcher().WithAsOf(indexTime("2026-11-02T14:50:00+08:00"))
		f.HTTP = &http.Client{Transport: indexTestTransport(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			fs := req.URL.Query().Get("fs")
			if !strings.Contains(fs, "i:100.NDX100") || !strings.Contains(fs, "i:100.KS11") || strings.Contains(fs, "i:100.NDX,") {
				t.Errorf("wrong symbols %s", fs)
			}
			rows := []map[string]interface{}{}
			for _, code := range []string{"SPX", "NDX100", "DJIA", "N225", "KS11", "HSI"} {
				stamp := indexTime("2026-10-30T16:00:00-04:00")
				if code == "N225" || code == "KS11" || code == "HSI" {
					stamp = indexTime("2026-11-02T14:49:00+08:00")
				}
				rows = append(rows, map[string]interface{}{"f12": code, "f14": code, "f2": 110, "f18": 100, "f124": stamp.Unix()})
			}
			var diff interface{} = rows
			if object {
				m := map[string]interface{}{}
				for i, row := range rows {
					m[string(rune('0'+i))] = row
				}
				diff = m
			}
			body, _ := json.Marshal(map[string]interface{}{"rc": 0, "data": map[string]interface{}{"diff": diff}})
			return indexResponse(200, string(body)), nil
		})}
		got := f.FetchIndex([]string{"SPX", "NDX", "DJI", "N225", "KOSPI", "HSI"})
		if len(got) != 6 || calls != 1 {
			t.Fatalf("quotes=%d calls=%d", len(got), calls)
		}
		for key, q := range got {
			if q.Source != "eastmoney" || q.PrevClose != 100 || math.Abs(q.ChangePct-.1) > 1e-9 || q.TradingDate == "" {
				t.Fatalf("%s: %+v", key, q)
			}
		}
	}
}

func TestYahooFallbackUsesNasdaq100AndPriorBarNotWindowBase(t *testing.T) {
	f := NewIndexFetcher().WithAsOf(indexTime("2026-11-02T08:50:00+08:00"))
	f.HTTP = &http.Client{Transport: indexTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "push2.eastmoney.com" {
			return indexResponse(503, "unavailable"), nil
		}
		if !strings.HasSuffix(req.URL.Path, "/^NDX") || req.URL.Query().Get("range") != "5d" {
			t.Errorf("wrong yahoo URL %s", req.URL)
		}
		return indexResponse(200, `{"chart":{"result":[{"meta":{"symbol":"^NDX","regularMarketPrice":110,"regularMarketTime":1793390400,"chartPreviousClose":50,"previousClose":60},"timestamp":[1793280600,1793367000],"indicators":{"quote":[{"close":[100,110]}]}}],"error":null}}`), nil
	})}
	got := f.FetchIndex([]string{"NDX"})["NDX"]
	if got.Source != "yahoo" || got.Last != 110 || got.PrevClose != 100 || math.Abs(got.ChangePct-.1) > 1e-9 {
		t.Fatalf("wrong fallback %+v", got)
	}
	if len(got.Failures) != 1 || !strings.Contains(got.Failures[0], "HTTP 503") {
		t.Fatal("failure diagnostics lost")
	}
}

func TestYahooHistoricalAnchorNeverUsesSameDayFutureClose(t *testing.T) {
	f := NewIndexFetcher().WithAsOf(indexTime("2026-11-02T09:30:00+08:00"))
	stamp := indexTime("2026-11-02T15:30:00+09:00").Unix()
	ts := []int64{indexTime("2026-10-29T09:00:00+09:00").Unix(), indexTime("2026-10-30T09:00:00+09:00").Unix(), indexTime("2026-11-02T09:00:00+09:00").Unix()}
	data := map[string]interface{}{"chart": map[string]interface{}{"result": []interface{}{map[string]interface{}{"meta": map[string]interface{}{"symbol": "^N225", "regularMarketPrice": 600, "regularMarketTime": stamp}, "timestamp": ts, "indicators": map[string]interface{}{"quote": []interface{}{map[string]interface{}{"close": []interface{}{480, 500, 600}}}}}}}}
	b, _ := json.Marshal(data)
	f.HTTP = &http.Client{Transport: indexTestTransport(func(*http.Request) (*http.Response, error) { return indexResponse(200, string(b)), nil })}
	q, err := f.fetchFromYahoo("N225", "query1.finance.yahoo.com")
	if err != nil {
		t.Fatal(err)
	}
	if q.Last != 500 || q.PrevClose != 480 || q.Status != "previous_session" || q.TradingDate != "2026-10-30" {
		t.Fatalf("future close leaked: %+v", q)
	}
}

func TestYahooIgnoresNullBarsAndUnfinishedUSSession(t *testing.T) {
	f := NewIndexFetcher().WithAsOf(indexTime("2026-11-02T12:00:00-05:00"))
	timestamps := []int64{indexTime("2026-10-28T09:30:00-04:00").Unix(), indexTime("2026-10-29T09:30:00-04:00").Unix(), indexTime("2026-10-30T09:30:00-04:00").Unix(), indexTime("2026-11-02T09:30:00-05:00").Unix()}
	data := map[string]interface{}{"chart": map[string]interface{}{"result": []interface{}{map[string]interface{}{"meta": map[string]interface{}{"symbol": "^GSPC", "regularMarketPrice": 220, "regularMarketTime": indexTime("2026-11-02T11:59:00-05:00").Unix()}, "timestamp": timestamps, "indicators": map[string]interface{}{"quote": []interface{}{map[string]interface{}{"close": []interface{}{180, 200, nil, 220}}}}}}}}
	b, _ := json.Marshal(data)
	f.HTTP = &http.Client{Transport: indexTestTransport(func(*http.Request) (*http.Response, error) { return indexResponse(200, string(b)), nil })}
	q, err := f.fetchFromYahoo("SPX", "query1.finance.yahoo.com")
	if err != nil {
		t.Fatal(err)
	}
	if q.Last != 200 || q.PrevClose != 180 || q.TradingDate != "2026-10-29" {
		t.Fatalf("wrong completed session %+v", q)
	}
}

func TestIndexQuoteTimeValidation(t *testing.T) {
	anchor := indexTime("2026-11-03T07:00:00+08:00")
	closedUS := IndexQuote{Symbol: "SPX", Last: 110, PrevClose: 100, Time: indexTime("2026-11-02T16:00:00-05:00")}
	if err := ValidateIndexQuoteAsOf(closedUS, anchor); err != nil {
		t.Fatalf("valid US close on same UTC date rejected: %v", err)
	}
	for _, test := range []struct {
		name string
		q    IndexQuote
		asOf time.Time
	}{
		{"future_asia", IndexQuote{Symbol: "N225", Last: 110, PrevClose: 100, Time: indexTime("2026-11-02T15:30:00+09:00")}, indexTime("2026-11-02T09:30:00+08:00")},
		{"stale", IndexQuote{Symbol: "N225", Last: 110, PrevClose: 100, Time: anchor.Add(-8 * 24 * time.Hour)}, anchor},
		{"missing_time", IndexQuote{Symbol: "SPX", Last: 110, PrevClose: 100}, anchor},
		{"missing_previous_close", IndexQuote{Symbol: "SPX", Last: 110, Time: closedUS.Time}, anchor},
		{"unfinished_US", IndexQuote{Symbol: "SPX", Last: 110, PrevClose: 100, Time: indexTime("2026-11-02T11:00:00-05:00")}, indexTime("2026-11-02T12:00:00-05:00")},
		{"stale_intraday_not_close", IndexQuote{Symbol: "SPX", Last: 110, PrevClose: 100, Time: indexTime("2026-11-02T14:00:00-05:00")}, anchor},
	} {
		t.Run(test.name, func(t *testing.T) {
			if ValidateIndexQuoteAsOf(test.q, test.asOf) == nil {
				t.Fatal("invalid quote accepted")
			}
		})
	}
}

func TestIndexFetcherPreservesHTTPFailures(t *testing.T) {
	f := NewIndexFetcher()
	f.HTTP = &http.Client{Transport: indexTestTransport(func(r *http.Request) (*http.Response, error) { return indexResponse(429, "rate limited"), nil })}
	q := f.FetchIndex([]string{"KOSPI"})["KOSPI"]
	if q.Last != 0 || q.Source != "unavailable" || len(q.Failures) != 3 || !strings.Contains(q.Error, "HTTP 429") {
		t.Fatalf("missing diagnostics %+v", q)
	}
}

func TestTencentUsesActualTimestampInBothMarketFormats(t *testing.T) {
	for _, stamp := range []string{"20261102144900", "2026/11/02 14:49:00"} {
		f := NewIndexFetcher().WithAsOf(indexTime("2026-11-02T14:50:00+08:00"))
		parts := make([]string, 31)
		parts[3] = "110"
		parts[4] = "100"
		parts[30] = stamp
		f.HTTP = &http.Client{Transport: indexTestTransport(func(*http.Request) (*http.Response, error) {
			return indexResponse(200, `v_hkHSI="`+strings.Join(parts, "~")+`";`), nil
		})}
		q, err := f.fetchFromTencent("HSI")
		if err != nil {
			t.Fatal(err)
		}
		if !q.Time.Equal(indexTime("2026-11-02T14:49:00+08:00")) || q.Status != "intraday" {
			t.Fatalf("timestamp replaced or misread %+v", q)
		}
	}
}
