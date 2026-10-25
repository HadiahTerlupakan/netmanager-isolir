// Package server answers every request from a suspended customer with the
// isolir page chosen by the connection's source address.
package server

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/hadiahterlupakan/netmanager-isolir/internal/page"
)

// Pages yields the HTML for a customer address.
type Pages interface {
	Page(ctx context.Context, address string) []byte
}

// NewHandler serves the isolir page for any method, host and path. The page is
// chosen only by r.RemoteAddr: headers such as X-Forwarded-For come from the
// customer and would let one customer read another's page.
func NewHandler(pages Pages, limiter *Limiter, stats *Stats) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		source := hostOf(r.RemoteAddr)
		stats.Record(source)
		if !limiter.Allow(source) {
			http.Error(w, "terlalu banyak permintaan", http.StatusTooManyRequests)
			return
		}
		body := page.FallbackHTML
		if ip, err := netip.ParseAddr(source); err == nil && ip.Unmap().Is4() {
			body = pages.Page(r.Context(), ip.Unmap().String())
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})
}
