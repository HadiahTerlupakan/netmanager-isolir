package server

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"
)

// Stats counts requests per source address between two flushes.
type Stats struct {
	mu     sync.Mutex
	counts map[string]int
}

// NewStats returns an empty Stats.
func NewStats() *Stats {
	return &Stats{counts: make(map[string]int)}
}

// Record counts one request from address.
func (s *Stats) Record(address string) {
	s.mu.Lock()
	s.counts[address]++
	s.mu.Unlock()
}

// Flush returns the counts so far and starts over.
func (s *Stats) Flush() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.counts
	s.counts = make(map[string]int)
	return out
}

// LogEvery writes one log line per address every interval until ctx ends.
func (s *Stats) LogEvery(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			counts := s.Flush()
			addrs := make([]string, 0, len(counts))
			for a := range counts {
				addrs = append(addrs, a)
			}
			sort.Strings(addrs)
			for _, a := range addrs {
				log.Printf("permintaan ip=%s jumlah=%d", a, counts[a])
			}
		}
	}
}
