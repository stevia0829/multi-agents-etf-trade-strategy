package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Resilient 提供「重试 + 多级 provider 降级 + 静态兜底」的统一容错调用。
//
// 调用顺序：
//   primary (重试 N 次) → fallback[0] (重试 N 次) → fallback[1] ... → static fallback
//
// 任何一级成功立即返回；全部失败时返回最后一次错误以及静态兜底文本（若提供）。
type Resilient struct {
	primary    Client
	fallbacks  []Client
	maxRetries int
	baseDelay  time.Duration
	static     StaticFallback
	logger     *log.Logger
}

// StaticFallback 在所有 LLM 都不可达时调用，返回一个降级回答（一般是规则模型）。
// 入参带上 system / user，便于规则函数按提示语决定返回。
type StaticFallback func(system, user string) string

type ResilientOption func(*Resilient)

func WithFallbacks(fbs ...Client) ResilientOption {
	return func(r *Resilient) { r.fallbacks = append(r.fallbacks, fbs...) }
}

func WithMaxRetries(n int) ResilientOption {
	return func(r *Resilient) { r.maxRetries = n }
}

func WithBaseDelay(d time.Duration) ResilientOption {
	return func(r *Resilient) { r.baseDelay = d }
}

func WithStaticFallback(f StaticFallback) ResilientOption {
	return func(r *Resilient) { r.static = f }
}

func WithLogger(l *log.Logger) ResilientOption {
	return func(r *Resilient) { r.logger = l }
}

func NewResilient(primary Client, opts ...ResilientOption) *Resilient {
	r := &Resilient{
		primary:    primary,
		maxRetries: 2,
		baseDelay:  500 * time.Millisecond,
		logger:     log.Default(),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *Resilient) Name() string {
	return "resilient(" + r.primary.Name() + ")"
}

func (r *Resilient) Chat(ctx context.Context, system, user string, opts ...ChatOptions) (string, error) {
	clients := append([]Client{r.primary}, r.fallbacks...)
	var lastErr error

	for i, c := range clients {
		out, err := r.callWithRetry(ctx, c, system, user, opts...)
		if err == nil {
			if i > 0 {
				r.logf("[llm] degraded to fallback #%d: %s", i, c.Name())
			}
			return out, nil
		}
		lastErr = err
		r.logf("[llm] provider %s failed: %v", c.Name(), err)

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			break
		}
	}

	if r.static != nil {
		r.logf("[llm] all providers failed, use static fallback")
		return r.static(system, user), nil
	}
	return "", fmt.Errorf("all providers failed, last error: %w", lastErr)
}

func (r *Resilient) callWithRetry(ctx context.Context, c Client, system, user string, opts ...ChatOptions) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if attempt > 0 {
			delay := r.baseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}
		out, err := c.Chat(ctx, system, user, opts...)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return "", err
		}
	}
	return "", lastErr
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "rate limited"),
		strings.Contains(s, "upstream 5"),
		strings.Contains(s, "timeout"),
		strings.Contains(s, "EOF"),
		strings.Contains(s, "connection reset"),
		strings.Contains(s, "i/o timeout"),
		strings.Contains(s, "no such host"):
		return true
	}
	return false
}

func (r *Resilient) logf(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Printf(format, args...)
	}
}
