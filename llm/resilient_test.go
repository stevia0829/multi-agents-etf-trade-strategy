package llm

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

type fakeClient struct {
	name    string
	failN   int // 前 N 次返回错误
	called  int
	err     error
	respond string
}

func (f *fakeClient) Name() string { return f.name }
func (f *fakeClient) Chat(_ context.Context, _ string, _ string, _ ...ChatOptions) (string, error) {
	f.called++
	if f.called <= f.failN {
		return "", f.err
	}
	return f.respond, nil
}

func TestResilient_RetrySuccess(t *testing.T) {
	primary := &fakeClient{name: "primary", failN: 1, err: errors.New("upstream 502"), respond: "ok"}
	r := NewResilient(primary,
		WithMaxRetries(2),
		WithBaseDelay(time.Millisecond),
		WithLogger(log.New(io.Discard, "", 0)),
	)
	got, err := r.Chat(context.Background(), "sys", "u")
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if got != "ok" {
		t.Fatalf("want ok, got %q", got)
	}
	if primary.called != 2 {
		t.Fatalf("expect 2 calls, got %d", primary.called)
	}
}

func TestResilient_NonRetryableSkipsRetry(t *testing.T) {
	primary := &fakeClient{name: "p", failN: 999, err: errors.New("invalid api key")}
	fb := &fakeClient{name: "fb", respond: "fallback"}
	r := NewResilient(primary,
		WithFallbacks(fb),
		WithMaxRetries(3),
		WithBaseDelay(time.Millisecond),
		WithLogger(log.New(io.Discard, "", 0)),
	)
	got, err := r.Chat(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "fallback" {
		t.Fatalf("want fallback, got %q", got)
	}
	if primary.called != 1 {
		t.Fatalf("non-retryable should not retry, called=%d", primary.called)
	}
}

func TestResilient_StaticFallback(t *testing.T) {
	primary := &fakeClient{name: "p", failN: 999, err: errors.New("upstream 503")}
	fb := &fakeClient{name: "fb", failN: 999, err: errors.New("rate limited")}
	r := NewResilient(primary,
		WithFallbacks(fb),
		WithMaxRetries(1),
		WithBaseDelay(time.Millisecond),
		WithStaticFallback(func(s, u string) string { return "{}" }),
		WithLogger(log.New(io.Discard, "", 0)),
	)
	got, err := r.Chat(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("expect static fallback to swallow error, got %v", err)
	}
	if got != "{}" {
		t.Fatalf("want {}, got %q", got)
	}
}

func TestIsRetryable(t *testing.T) {
	cases := map[string]bool{
		"rate limited":     true,
		"upstream 503":     true,
		"i/o timeout":      true,
		"no such host":     true,
		"invalid api key":  false,
		"some random err":  false,
	}
	for s, want := range cases {
		if got := isRetryable(errors.New(s)); got != want {
			t.Errorf("isRetryable(%q)=%v want %v", s, got, want)
		}
	}
}
