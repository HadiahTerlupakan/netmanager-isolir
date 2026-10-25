package page

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	html      []byte
	etag      string
	fetchedAt time.Time
}

// call is a fetch in progress; done closes once the entry has been updated or
// the fetch has failed.
type call struct{ done chan struct{} }

// Cache keeps the page per customer address and refreshes it from a Fetcher.
type Cache struct {
	fetcher      Fetcher
	ttl          time.Duration
	staleFor     time.Duration
	fetchTimeout time.Duration
	now          func() time.Time

	mu       sync.Mutex
	entries  map[string]entry
	inflight map[string]*call
}

// NewCache returns a Cache that revalidates an entry older than ttl, reuses it
// for up to staleFor when the fetch fails, and gives each fetch fetchTimeout.
func NewCache(f Fetcher, ttl, staleFor, fetchTimeout time.Duration, now func() time.Time) *Cache {
	return &Cache{
		fetcher:      f,
		ttl:          ttl,
		staleFor:     staleFor,
		fetchTimeout: fetchTimeout,
		now:          now,
		entries:      make(map[string]entry),
		inflight:     make(map[string]*call),
	}
}

// Page returns the HTML for address. It never fails: when netmanager cannot
// help it returns an older copy, and past staleFor the fallback page.
func (c *Cache) Page(ctx context.Context, address string) []byte {
	c.mu.Lock()
	e, ok := c.entries[address]
	if ok && c.now().Sub(e.fetchedAt) < c.ttl {
		c.mu.Unlock()
		return e.html
	}
	cl, running := c.inflight[address]
	if !running {
		cl = &call{done: make(chan struct{})}
		c.inflight[address] = cl
		go c.refresh(ctx, address, e.etag, cl)
	}
	c.mu.Unlock()

	select {
	case <-cl.done:
	case <-ctx.Done():
	}
	return c.usable(address)
}

// refresh runs detached from the first caller's cancellation so that waiters
// sharing the call do not lose the result when that caller goes away.
func (c *Cache) refresh(ctx context.Context, address, etag string, cl *call) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.fetchTimeout)
	defer cancel()
	res, err := c.fetcher.Fetch(ctx, address, etag)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.store(address, res)
	}
	delete(c.inflight, address)
	close(cl.done)
}

func (c *Cache) store(address string, res Result) {
	e, ok := c.entries[address]
	switch {
	case res.NotModified && ok:
		e.fetchedAt = c.now()
	case res.NotModified:
		return
	default:
		e = entry{html: res.HTML, etag: res.ETag, fetchedAt: c.now()}
	}
	c.entries[address] = e
}

func (c *Cache) usable(address string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[address]; ok && c.now().Sub(e.fetchedAt) < c.staleFor {
		return e.html
	}
	return FallbackHTML
}
