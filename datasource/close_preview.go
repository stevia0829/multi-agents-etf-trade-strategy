package datasource

import (
	"fmt"
	"sync"
	"time"

	"github.com/multi-agents-etf-trade-strategy/types"
)

const (
	defaultClosePreviewMaxQuoteAge = 2 * time.Minute
	closePreviewMaxSnapshotAge     = 6 * time.Minute
)

// ClosePreviewDataSource 将 14:50 左右的实时价写入当天临时日 K，供尾盘一次性决策使用。
// 机理：21 日动量只需要每日收盘序列，尾盘实时价可作为当天收盘的近似值。
// 适用场景：所有 Regime；失效场景是行情延迟、当天日 K 缺失或尾盘剧烈反转，此时直接报错而非回退 T-1。
type ClosePreviewDataSource struct {
	inner       ETFDataSource
	quotes      RealtimeQuoter
	now         func() time.Time
	maxQuoteAge time.Duration

	mu        sync.Mutex
	quoteTime map[string]time.Time
}

// NewClosePreviewDataSource 创建严格尾盘数据源；底层必须同时支持日 K 和实时报价。
func NewClosePreviewDataSource(inner ETFDataSource) (*ClosePreviewDataSource, error) {
	quotes, ok := inner.(RealtimeQuoter)
	if !ok {
		return nil, fmt.Errorf("close preview: datasource does not support realtime quotes")
	}
	return &ClosePreviewDataSource{
		inner:       inner,
		quotes:      quotes,
		now:         time.Now,
		maxQuoteAge: defaultClosePreviewMaxQuoteAge,
		quoteTime:   make(map[string]time.Time),
	}, nil
}

func (d *ClosePreviewDataSource) ListAllETFs() ([]types.ETF, error) {
	return d.inner.ListAllETFs()
}

func (d *ClosePreviewDataSource) GetKLine(code string, days int) ([]types.KLine, error) {
	return d.GetKLineAsOf(code, days, time.Time{})
}

// GetKLineAsOf 只修正实时请求；历史 AsOf 请求保持原样，避免污染回测口径。
func (d *ClosePreviewDataSource) GetKLineAsOf(code string, days int, asOf time.Time) ([]types.KLine, error) {
	klines, err := d.inner.GetKLineAsOf(code, days, asOf)
	if err != nil || !asOf.IsZero() {
		return klines, err
	}
	if len(klines) == 0 {
		return nil, fmt.Errorf("close preview %s: empty kline", code)
	}

	q, err := d.fetchAndValidateQuote(code)
	if err != nil {
		return nil, err
	}
	now := d.now()
	last := klines[len(klines)-1]
	if !sameLocalDate(last.Date, now) {
		return nil, fmt.Errorf("close preview %s: latest kline is %s, want %s",
			code, last.Date.Format("2006-01-02"), now.Format("2006-01-02"))
	}

	out := append([]types.KLine(nil), klines...)
	last.Close = q.Price
	if last.High <= 0 || q.Price > last.High {
		last.High = q.Price
	}
	if last.Low <= 0 || q.Price < last.Low {
		last.Low = q.Price
	}
	last.IsPartial = true
	out[len(out)-1] = last
	return out, nil
}

// FetchRealtimeQuote 透传并执行与核心 K 线一致的时效校验。
func (d *ClosePreviewDataSource) FetchRealtimeQuote(code string) (RealtimeQuote, error) {
	return d.fetchAndValidateQuote(code)
}

func (d *ClosePreviewDataSource) fetchAndValidateQuote(code string) (RealtimeQuote, error) {
	q, err := d.quotes.FetchRealtimeQuote(code)
	if err != nil {
		return q, fmt.Errorf("close preview %s quote: %w", code, err)
	}
	now := d.now()
	if q.Time.IsZero() {
		return q, fmt.Errorf("close preview %s: quote timestamp is missing", code)
	}
	if !sameLocalDate(q.Time, now) {
		return q, fmt.Errorf("close preview %s: quote date is %s, want %s",
			code, q.Time.Format("2006-01-02"), now.Format("2006-01-02"))
	}
	age := now.Sub(q.Time)
	if age < -30*time.Second || age > d.maxQuoteAge {
		return q, fmt.Errorf("close preview %s: stale quote time=%s age=%s",
			code, q.Time.Format("15:04:05"), age.Round(time.Second))
	}
	d.mu.Lock()
	d.quoteTime[code] = q.Time
	d.mu.Unlock()
	return q, nil
}

// SnapshotTime 返回本轮核心计算使用的最早报价时间；零值表示尚无有效报价。
func (d *ClosePreviewDataSource) SnapshotTime() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	var earliest time.Time
	for _, t := range d.quoteTime {
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

// ValidateSnapshot 确保流水线结束时最早的核心报价仍在允许时效内。
func (d *ClosePreviewDataSource) ValidateSnapshot(now time.Time) error {
	earliest := d.SnapshotTime()
	if earliest.IsZero() {
		return fmt.Errorf("close preview: no validated realtime quote")
	}
	if age := now.Sub(earliest); age > closePreviewMaxSnapshotAge {
		return fmt.Errorf("close preview: pipeline finished with stale snapshot time=%s age=%s",
			earliest.Format("15:04:05"), age.Round(time.Second))
	}
	return nil
}

func sameLocalDate(a, b time.Time) bool {
	ay, am, ad := a.In(time.Local).Date()
	by, bm, bd := b.In(time.Local).Date()
	return ay == by && am == bm && ad == bd
}

var _ ETFDataSource = (*ClosePreviewDataSource)(nil)
var _ RealtimeQuoter = (*ClosePreviewDataSource)(nil)
