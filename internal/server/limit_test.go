package server

import (
	"net"
	"testing"
	"time"
)

func TestLimiterRefillsOverTime(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(20, 2, 10)
	l.now = func() time.Time { return now }
	if !l.Allow("a") || !l.Allow("a") || l.Allow("a") {
		t.Fatal("burst of 2 not enforced")
	}
	now = now.Add(50 * time.Millisecond)
	if !l.Allow("a") {
		t.Error("one token should have refilled after 50ms at 20/s")
	}
	if l.Allow("a") {
		t.Error("only one token should have refilled")
	}
}

func TestLimiterForgetsIdleIPs(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(20, 2, 10)
	l.now = func() time.Time { return now }
	l.Allow("a")
	now = now.Add(2 * idleAfter)
	l.Allow("b")
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.buckets["a"]; ok {
		t.Error("idle bucket kept")
	}
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func acceptLoop(ln net.Listener) <-chan net.Conn {
	ch := make(chan net.Conn, 64)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				close(ch)
				return
			}
			ch <- c
		}
	}()
	return ch
}

func next(t *testing.T, ch <-chan net.Conn) net.Conn {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("accept timed out")
		return nil
	}
}

func TestWrapListenerClosesConnectionBeyondLimit(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := NewLimiter(1000, 1000, 32).WrapListener(raw)
	defer ln.Close()
	accepted := acceptLoop(ln)

	var held []net.Conn
	for i := 0; i < 32; i++ {
		held = append(held, dial(t, raw.Addr().String()), next(t, accepted))
	}
	extra := dial(t, raw.Addr().String())
	defer extra.Close()
	extra.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := extra.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Fatalf("33rd connection should be closed by the server, got %v", err)
	}

	held[1].Close() // a server-side connection of the 32 is released
	again := dial(t, raw.Addr().String())
	defer again.Close()
	next(t, accepted).Close()
	for _, c := range held {
		c.Close()
	}
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}
