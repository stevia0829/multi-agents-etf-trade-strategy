package datasource

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // 保证精简服务端镜像也能正确处理美股夏令时。
)

// IndexQuote 保留真实报价时间、市场交易日和来源；失败不得伪造价格或当前时间。
type IndexQuote struct {
	Symbol, Name                       string
	Last, PrevClose, Change, ChangePct float64
	Time                               time.Time
	Source, TradingDate, Status        string
	Error                              string
	Failures                           []string
}

type IndexFetcher struct {
	HTTP *http.Client
	AsOf time.Time
}

func NewIndexFetcher() *IndexFetcher {
	return &IndexFetcher{HTTP: &http.Client{Timeout: 8 * time.Second}}
}
func (f *IndexFetcher) WithAsOf(t time.Time) *IndexFetcher { f.AsOf = t; return f }

var indexNames = map[string]string{"SPX": "标普500", "NDX": "纳斯达克100", "DJI": "道琼斯", "N225": "日经225", "KOSPI": "韩国KOSPI", "HSI": "恒生指数", "SH": "上证综指"}

// 东方财富100.NDX是纳斯达克综合指数，100.NDX100才是纳斯达克100。
// 映射经其公开证券搜索与行情接口交叉核对，不能用相近指数代替。
var eastmoneyIndexMap = map[string]string{"SPX": "100.SPX", "NDX": "100.NDX100", "DJI": "100.DJIA", "N225": "100.N225", "KOSPI": "100.KS11", "HSI": "100.HSI", "SH": "1.000001"}
var yahooSymbolMap = map[string]string{"SPX": "^GSPC", "NDX": "^NDX", "DJI": "^DJI", "N225": "^N225", "KOSPI": "^KS11", "HSI": "^HSI", "SH": "000001.SS"}
var indexSymbolMap = map[string]string{"HSI": "hkHSI", "SH": "sh000001"}

func indexLocation(symbol string) *time.Location {
	name := "Asia/Shanghai"
	switch symbol {
	case "SPX", "NDX", "DJI":
		name = "America/New_York"
	case "N225":
		name = "Asia/Tokyo"
	case "KOSPI":
		name = "Asia/Seoul"
	case "HSI":
		name = "Asia/Hong_Kong"
	}
	loc, _ := time.LoadLocation(name)
	return loc
}

func indexSessionClose(symbol string, stamp time.Time) time.Time {
	t := stamp.In(indexLocation(symbol))
	h, m := 16, 0
	switch symbol {
	case "N225", "KOSPI":
		h, m = 15, 30
	case "SH":
		h, m = 15, 0
	}
	return time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, t.Location())
}

// ValidateIndexQuoteAsOf 严格按实际时点过滤，不能把当日下午收盘倒灌为早盘报价。
// 最近交易日可跨周末/休市，状态必须标为previous_session，最多容忍7个自然日。
func ValidateIndexQuoteAsOf(q IndexQuote, asOf time.Time) error {
	if q.Last <= 0 || q.PrevClose <= 0 {
		return fmt.Errorf("missing valid price/previous close")
	}
	if q.Time.IsZero() {
		return fmt.Errorf("missing source timestamp")
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	if q.Time.After(asOf) {
		return fmt.Errorf("quote %s is later than query %s", q.Time.Format(time.RFC3339), asOf.Format(time.RFC3339))
	}
	if asOf.Sub(q.Time) > 7*24*time.Hour {
		return fmt.Errorf("quote is older than 7 days: %s", q.Time.Format(time.RFC3339))
	}
	if q.Symbol == "SPX" || q.Symbol == "NDX" || q.Symbol == "DJI" {
		closeTime := indexSessionClose(q.Symbol, q.Time)
		if closeTime.After(asOf) {
			return fmt.Errorf("US session has not closed at query time")
		}
		if q.Time.Before(closeTime.Add(-time.Minute)) {
			return fmt.Errorf("US quote is an intraday tick, not a close snapshot")
		}
	}
	return nil
}

func finishIndexQuote(q IndexQuote, asOf time.Time) (IndexQuote, error) {
	if err := ValidateIndexQuoteAsOf(q, asOf); err != nil {
		return q, err
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	loc := indexLocation(q.Symbol)
	q.TradingDate = q.Time.In(loc).Format("2006-01-02")
	q.Status = "closed"
	if q.TradingDate != asOf.In(loc).Format("2006-01-02") {
		q.Status = "previous_session"
	} else if indexSessionClose(q.Symbol, q.Time).After(asOf) || q.Time.Before(indexSessionClose(q.Symbol, q.Time).Add(-time.Minute)) {
		q.Status = "intraday"
	}
	q.Change = q.Last - q.PrevClose
	q.ChangePct = q.Change / q.PrevClose
	return q, nil
}

func (f *IndexFetcher) get(rawURL, referer string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	client := f.HTTP
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

// FetchIndex 主源一次批量查询东方财富，失败项目再并发尝试腾讯及Yahoo。
// Stooq旧CSV入口已实测404，且旧实现误用开盘价作昨收，故不再参与降级链。
func (f *IndexFetcher) FetchIndex(symbols []string) map[string]IndexQuote {
	unique := map[string]bool{}
	keys := []string{}
	for _, s := range symbols {
		k := strings.ToUpper(strings.TrimSpace(s))
		if k != "" && !unique[k] {
			unique[k] = true
			keys = append(keys, k)
		}
	}
	primary, primaryErr := f.fetchFromEastMoney(keys)
	out := map[string]IndexQuote{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			failures := []string{}
			q, ok := primary[k]
			if ok && q.Last > 0 && q.Error == "" {
				mu.Lock()
				out[k] = q
				mu.Unlock()
				return
			}
			if ok && q.Error != "" {
				failures = append(failures, "eastmoney: "+q.Error)
			} else if primaryErr != nil {
				failures = append(failures, "eastmoney: "+primaryErr.Error())
			} else {
				failures = append(failures, "eastmoney: missing or invalid quote")
			}
			if _, supported := indexSymbolMap[k]; supported {
				var err error
				q, err = f.fetchFromTencent(k)
				if err == nil {
					q.Failures = failures
					mu.Lock()
					out[k] = q
					mu.Unlock()
					return
				}
				failures = append(failures, "tencent: "+err.Error())
			}
			if _, supported := yahooSymbolMap[k]; supported {
				for _, host := range []string{"query1.finance.yahoo.com", "query2.finance.yahoo.com"} {
					var err error
					q, err = f.fetchFromYahoo(k, host)
					if err == nil {
						q.Failures = failures
						mu.Lock()
						out[k] = q
						mu.Unlock()
						return
					}
					failures = append(failures, host+": "+err.Error())
				}
			}
			q = IndexQuote{Symbol: k, Name: indexNames[k], Source: "unavailable", Error: strings.Join(failures, "; "), Failures: failures}
			mu.Lock()
			out[k] = q
			mu.Unlock()
		}(key)
	}
	wg.Wait()
	return out
}

func (f *IndexFetcher) fetchFromEastMoney(symbols []string) (map[string]IndexQuote, error) {
	ids := []string{}
	byCode := map[string]string{}
	for _, key := range symbols {
		if id, ok := eastmoneyIndexMap[key]; ok {
			ids = append(ids, "i:"+id)
			byCode[strings.SplitN(id, ".", 2)[1]] = key
		}
	}
	out := map[string]IndexQuote{}
	if len(ids) == 0 {
		return out, fmt.Errorf("no supported symbols")
	}
	values := url.Values{"np": {"2"}, "fltt": {"2"}, "invt": {"2"}, "pn": {"1"}, "pz": {"200"}, "fs": {strings.Join(ids, ",")}, "fields": {"f12,f14,f2,f18,f3,f4,f124"}}
	body, err := f.get("https://push2.eastmoney.com/api/qt/clist/get?"+values.Encode(), "https://quote.eastmoney.com/")
	if err != nil {
		return out, err
	}
	var raw struct {
		RC   int `json:"rc"`
		Data struct {
			Diff json.RawMessage `json:"diff"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &raw); err != nil {
		return out, fmt.Errorf("invalid JSON: %w", err)
	}
	if raw.RC != 0 {
		return out, fmt.Errorf("API rc=%d", raw.RC)
	}
	var rows []map[string]interface{}
	if err = json.Unmarshal(raw.Data.Diff, &rows); err != nil {
		var obj map[string]map[string]interface{}
		if err = json.Unmarshal(raw.Data.Diff, &obj); err != nil {
			return out, fmt.Errorf("missing quote rows")
		}
		for _, row := range obj {
			rows = append(rows, row)
		}
	}
	for _, r := range rows {
		key, ok := byCode[toStr(r["f12"])]
		if !ok {
			continue
		}
		stamp := int64(toFloat(r["f124"]))
		if stamp <= 0 {
			continue
		}
		q := IndexQuote{Symbol: key, Name: toStr(r["f14"]), Last: toFloat(r["f2"]), PrevClose: toFloat(r["f18"]), Time: time.Unix(stamp, 0), Source: "eastmoney"}
		if q, err = finishIndexQuote(q, f.AsOf); err == nil {
			out[key] = q
		} else {
			q.Last = 0
			q.Error = err.Error()
			out[key] = q
		}
	}
	return out, nil
}

func (f *IndexFetcher) fetchFromTencent(symbol string) (IndexQuote, error) {
	code, ok := indexSymbolMap[symbol]
	if !ok {
		return IndexQuote{}, fmt.Errorf("unsupported symbol")
	}
	body, err := f.get("https://qt.gtimg.cn/q="+code, "https://stockapp.finance.qq.com/")
	if err != nil {
		return IndexQuote{}, err
	}
	text := string(body)
	start := strings.Index(text, `"`)
	end := strings.LastIndex(text, `"`)
	if start < 0 || end <= start {
		return IndexQuote{}, fmt.Errorf("invalid quote payload")
	}
	parts := strings.Split(text[start+1:end], "~")
	if len(parts) <= 30 {
		return IndexQuote{}, fmt.Errorf("missing timestamp field")
	}
	var stamp time.Time
	for _, layout := range []string{"20060102150405", "2006/01/02 15:04:05", "2006-01-02 15:04:05"} {
		if parsed, e := time.ParseInLocation(layout, parts[30], indexLocation(symbol)); e == nil {
			stamp = parsed
			break
		}
	}
	if stamp.IsZero() {
		return IndexQuote{}, fmt.Errorf("invalid source timestamp")
	}
	return finishIndexQuote(IndexQuote{Symbol: symbol, Name: indexNames[symbol], Last: parseF(parts[3]), PrevClose: parseF(parts[4]), Time: stamp, Source: "tencent"}, f.AsOf)
}

func (f *IndexFetcher) fetchFromYahoo(symbol, host string) (IndexQuote, error) {
	code, ok := yahooSymbolMap[symbol]
	if !ok {
		return IndexQuote{}, fmt.Errorf("unsupported symbol")
	}
	body, err := f.get("https://"+host+"/v8/finance/chart/"+url.PathEscape(code)+"?range=5d&interval=1d", "")
	if err != nil {
		return IndexQuote{}, err
	}
	var raw struct {
		Chart struct {
			Error  json.RawMessage `json:"error"`
			Result []struct {
				Meta struct {
					Symbol        string   `json:"symbol"`
					Price         float64  `json:"regularMarketPrice"`
					Time          int64    `json:"regularMarketTime"`
					PreviousClose float64  `json:"previousClose"`
					ChangePct     *float64 `json:"regularMarketChangePercent"`
				} `json:"meta"`
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err = json.Unmarshal(body, &raw); err != nil {
		return IndexQuote{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(raw.Chart.Result) == 0 {
		return IndexQuote{}, fmt.Errorf("empty chart result")
	}
	r := raw.Chart.Result[0]
	if r.Meta.Symbol != code {
		return IndexQuote{}, fmt.Errorf("symbol mismatch: got %s want %s", r.Meta.Symbol, code)
	}
	anchor := f.AsOf
	if anchor.IsZero() {
		anchor = time.Now()
	}
	type bar struct {
		stamp time.Time
		close float64
	}
	bars := []bar{}
	if len(r.Indicators.Quote) > 0 {
		for i, stamp := range r.Timestamp {
			closes := r.Indicators.Quote[0].Close
			if i < len(closes) && closes[i] != nil && *closes[i] > 0 {
				bars = append(bars, bar{indexSessionClose(symbol, time.Unix(stamp, 0)), *closes[i]})
			}
		}
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].stamp.Before(bars[j].stamp) })
	q := IndexQuote{Symbol: symbol, Name: indexNames[symbol], Source: "yahoo", Last: r.Meta.Price}
	if r.Meta.Time > 0 {
		q.Time = time.Unix(r.Meta.Time, 0)
	}
	// 美股未收盘或最新tick晚于历史查询锚点时，只回退至已完成的日线。
	useMeta := q.Last > 0 && !q.Time.IsZero() && !q.Time.After(anchor)
	if symbol == "SPX" || symbol == "NDX" || symbol == "DJI" {
		closeTime := indexSessionClose(symbol, q.Time)
		useMeta = useMeta && !closeTime.After(anchor) && !q.Time.Before(closeTime.Add(-time.Minute))
	}
	if !useMeta {
		q.Last = 0
		q.Time = time.Time{}
		for _, b := range bars {
			if !b.stamp.After(anchor) {
				q.Last = b.close
				q.Time = b.stamp
			}
		}
	}
	if q.Time.IsZero() {
		return q, fmt.Errorf("no quote available before query time")
	}
	date := q.Time.In(indexLocation(symbol)).Format("2006-01-02")
	for _, b := range bars {
		if b.stamp.In(indexLocation(symbol)).Format("2006-01-02") < date && !b.stamp.After(anchor) {
			q.PrevClose = b.close
		}
	}
	if q.PrevClose <= 0 && useMeta {
		q.PrevClose = r.Meta.PreviousClose
		if q.PrevClose <= 0 && r.Meta.ChangePct != nil && 1+*r.Meta.ChangePct/100 > 0 {
			q.PrevClose = q.Last / (1 + *r.Meta.ChangePct/100)
		}
	}
	// chartPreviousClose是range窗口起点，不能当成逐日昨收。
	return finishIndexQuote(q, anchor)
}
