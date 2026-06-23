package datasource

import (
	"sync"
	"time"

	"github.com/eino-multi-etf-strategy/types"
)

// CachedDataSource 包装任意 ETFDataSource，对所有 GetKLineAsOf 调用做内存缓存。
// 用于回测对比场景：第二个变体复用第一个变体已拉取的数据，保证基线完全一致。
//
// 缓存 key = code|days|asOf(unix day)；命中直接返回副本，未命中穿透到底层源。
type CachedDataSource struct {
	inner ETFDataSource
	mu    sync.RWMutex
	cache map[string][]types.KLine
}

// NewCachedDataSource 创建缓存包装器。
func NewCachedDataSource(inner ETFDataSource) *CachedDataSource {
	return &CachedDataSource{
		inner: inner,
		cache: make(map[string][]types.KLine),
	}
}

func cacheKey(code string, days int, asOf time.Time) string {
	// asOf 零值（拉最新）用 0 表示，否则取日期部分的天数偏移
	d := int64(0)
	if !asOf.IsZero() {
		d = asOf.Unix() / 86400
	}
	return formatKey(code, days, d)
}

func formatKey(code string, days int, day int64) string {
	return code + "|" + itoa(days) + "|" + itoa64(day)
}

// 快速整数→字符串（避免引入 strconv 的开销，虽然差异极小）
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ListAllETFs 直接穿透到底层（ETF 列表本身不缓存，体积小且低频）。
func (c *CachedDataSource) ListAllETFs() ([]types.ETF, error) {
	return c.inner.ListAllETFs()
}

// GetKLine 直接穿透（不带 asOf 的便捷方法，回测不用这条路径）。
func (c *CachedDataSource) GetKLine(code string, days int) ([]types.KLine, error) {
	return c.inner.GetKLine(code, days)
}

// GetKLineAsOf 带缓存：命中返回副本，未命中穿透并缓存结果。
func (c *CachedDataSource) GetKLineAsOf(code string, days int, asOf time.Time) ([]types.KLine, error) {
	key := cacheKey(code, days, asOf)

	c.mu.RLock()
	if cached, ok := c.cache[key]; ok {
		c.mu.RUnlock()
		// 返回副本，防止调用方修改污染缓存
		out := make([]types.KLine, len(cached))
		copy(out, cached)
		return out, nil
	}
	c.mu.RUnlock()

	// 穿透到底层数据源
	data, err := c.inner.GetKLineAsOf(code, days, asOf)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = data
	c.mu.Unlock()

	// 返回副本
	out := make([]types.KLine, len(data))
	copy(out, data)
	return out, nil
}

// CacheStats 返回缓存统计（命中数 / 总条目数）。
func (c *CachedDataSource) CacheStats() (entries int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
