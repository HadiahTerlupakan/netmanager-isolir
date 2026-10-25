package page

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeFetcher struct {
	mu    sync.Mutex
	calls int
	etags []string
	fn    func(ctx context.Context, address, etag string) (Result, error)
}

func (f *fakeFetcher) Fetch(ctx context.Context, address, etag string) (Result, error) {
	f.mu.Lock()
	f.calls++
	f.etags = append(f.etags, etag)
	f.mu.Unlock()
	return f.fn(ctx, address, etag)
}

func (f *fakeFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeClock struct{ t atomic.Int64 }

func (c *fakeClock) now() time.Time          { return time.Unix(0, c.t.Load()) }
func (c *fakeClock) advance(d time.Duration) { c.t.Add(int64(d)) }

const (
	testTTL     = 60 * time.Second
	testStale   = 24 * time.Hour
	testTimeout = 50 * time.Millisecond
	addr        = "10.127.0.5"
)

func okFetcher(html string) *fakeFetcher {
	return &fakeFetcher{fn: func(context.Context, string, string) (Result, error) {
		return Result{HTML: []byte(html), ETag: `"v1"`}, nil
	}}
}

func newTestCache(f Fetcher, clk *fakeClock) *Cache {
	return NewCache(f, testTTL, testStale, testTimeout, clk.now)
}

func TestCacheServesFromCacheWithinTTL(t *testing.T) {
	f, clk := okFetcher("A"), &fakeClock{}
	c := newTestCache(f, clk)
	c.Page(context.Background(), addr)
	clk.advance(59 * time.Second)
	if got := c.Page(context.Background(), addr); string(got) != "A" {
		t.Fatalf("got %q", got)
	}
	if f.callCount() != 1 {
		t.Fatalf("calls = %d", f.callCount())
	}
}

func TestCacheRevalidatesWithETagAndNotModifiedExtends(t *testing.T) {
	f, clk := okFetcher("A"), &fakeClock{}
	c := newTestCache(f, clk)
	c.Page(context.Background(), addr)

	f.fn = func(context.Context, string, string) (Result, error) { return Result{NotModified: true}, nil }
	clk.advance(61 * time.Second)
	if got := c.Page(context.Background(), addr); string(got) != "A" {
		t.Fatalf("got %q", got)
	}
	if f.etags[1] != `"v1"` {
		t.Fatalf("etag sent = %q", f.etags[1])
	}
	clk.advance(30 * time.Second)
	c.Page(context.Background(), addr)
	if f.callCount() != 2 {
		t.Fatalf("calls = %d, 304 should have renewed the entry", f.callCount())
	}
}

func TestCacheReplacesHTMLOnNewVersion(t *testing.T) {
	f, clk := okFetcher("A"), &fakeClock{}
	c := newTestCache(f, clk)
	c.Page(context.Background(), addr)
	f.fn = func(context.Context, string, string) (Result, error) {
		return Result{HTML: []byte("B"), ETag: `"v2"`}, nil
	}
	clk.advance(61 * time.Second)
	if got := c.Page(context.Background(), addr); string(got) != "B" {
		t.Fatalf("got %q", got)
	}
}

func TestCacheStaleAndFallback(t *testing.T) {
	tests := []struct {
		name   string
		age    time.Duration
		primed bool
		want   []byte
	}{
		{"23h old page still served", 23 * time.Hour, true, []byte("A")},
		{"25h old page falls back", 25 * time.Hour, true, FallbackHTML},
		{"never fetched falls back", 0, false, FallbackHTML},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, clk := okFetcher("A"), &fakeClock{}
			c := newTestCache(f, clk)
			if tt.primed {
				c.Page(context.Background(), addr)
			}
			f.fn = func(context.Context, string, string) (Result, error) { return Result{}, errors.New("down") }
			clk.advance(tt.age)
			if got := c.Page(context.Background(), addr); !bytes.Equal(got, tt.want) {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func TestCacheHungFetcherReturnsWithinTimeout(t *testing.T) {
	f, clk := okFetcher("A"), &fakeClock{}
	c := newTestCache(f, clk)
	c.Page(context.Background(), addr)
	f.fn = func(ctx context.Context, _, _ string) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}
	clk.advance(2 * time.Minute)

	start := time.Now()
	got := c.Page(context.Background(), addr)
	if elapsed := time.Since(start); elapsed > testTimeout+100*time.Millisecond {
		t.Fatalf("took %v", elapsed)
	}
	if string(got) != "A" {
		t.Fatalf("got %q", got)
	}

	if got := c.Page(context.Background(), "10.127.0.6"); !bytes.Equal(got, FallbackHTML) {
		t.Fatalf("unknown address got %q", got)
	}
}

func TestCacheCoalescesConcurrentFetches(t *testing.T) {
	f, clk := okFetcher("A"), &fakeClock{}
	c := newTestCache(f, clk)
	c.Page(context.Background(), addr)

	release := make(chan struct{})
	f.fn = func(context.Context, string, string) (Result, error) {
		<-release
		return Result{HTML: []byte("B"), ETag: `"v2"`}, nil
	}
	clk.advance(2 * time.Minute)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Page(context.Background(), addr)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if f.callCount() != 2 {
		t.Fatalf("calls = %d, want 2 (prime + one refresh)", f.callCount())
	}
	if got := c.Page(context.Background(), addr); string(got) != "B" {
		t.Fatalf("got %q", got)
	}
}
