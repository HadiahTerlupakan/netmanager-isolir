package server

import (
	"net"
	"sync"
	"time"
)

// idleAfter is how long an address may stay quiet before its bucket is dropped,
// so the map does not grow with every address ever seen.
const idleAfter = time.Minute

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter bounds the request rate and the open connections per source address.
type Limiter struct {
	rate     float64
	burst    float64
	maxConns int
	now      func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	conns     map[string]int
	lastSweep time.Time
}

// NewLimiter allows ratePerSec requests per second with the given burst, and
// at most maxConnsPerIP simultaneous connections, for each address.
func NewLimiter(ratePerSec float64, burst int, maxConnsPerIP int) *Limiter {
	return &Limiter{
		rate:     ratePerSec,
		burst:    float64(burst),
		maxConns: maxConnsPerIP,
		now:      time.Now,
		buckets:  make(map[string]*bucket),
		conns:    make(map[string]int),
	}
}

// Allow takes one request token for address and reports whether it was there.
func (l *Limiter) Allow(address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	b, ok := l.buckets[address]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[address] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < idleAfter {
		return
	}
	l.lastSweep = now
	for a, b := range l.buckets {
		if now.Sub(b.last) > idleAfter {
			delete(l.buckets, a)
		}
	}
}

// WrapListener closes at once any connection that would exceed the limit of
// open connections for its source address.
func (l *Limiter) WrapListener(ln net.Listener) net.Listener {
	return &limitedListener{Listener: ln, limiter: l}
}

type limitedListener struct {
	net.Listener
	limiter *Limiter
}

func (ll *limitedListener) Accept() (net.Conn, error) {
	for {
		c, err := ll.Listener.Accept()
		if err != nil {
			return nil, err
		}
		address := hostOf(c.RemoteAddr().String())
		if !ll.limiter.acquire(address) {
			c.Close()
			continue
		}
		return &countedConn{Conn: c, limiter: ll.limiter, address: address}, nil
	}
}

func (l *Limiter) acquire(address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns[address] >= l.maxConns {
		return false
	}
	l.conns[address]++
	return true
}

func (l *Limiter) release(address string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns[address] <= 1 {
		delete(l.conns, address)
		return
	}
	l.conns[address]--
}

type countedConn struct {
	net.Conn
	limiter *Limiter
	address string
	once    sync.Once
}

func (c *countedConn) Close() error {
	c.once.Do(func() { c.limiter.release(c.address) })
	return c.Conn.Close()
}

func hostOf(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
