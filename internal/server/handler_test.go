package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hadiahterlupakan/netmanager-isolir/internal/page"
)

type fakePages struct {
	mu        sync.Mutex
	addresses []string
}

func (f *fakePages) Page(_ context.Context, address string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addresses = append(f.addresses, address)
	return []byte("halaman " + address)
}

func (f *fakePages) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.addresses...)
}

func newTestHandler(pages *fakePages, limiter *Limiter) http.Handler {
	return NewHandler(pages, limiter, NewStats())
}

func serve(h http.Handler, method, target, remote string, mod func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remote
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAnyRequestGetsThePageOfItsSourceIP(t *testing.T) {
	cases := []struct{ name, method, target, host string }{
		{"get path", "GET", "http://x/apa/saja", "x"},
		{"post", "POST", "/", "x"},
		{"head", "HEAD", "/", "x"},
		{"foreign host", "GET", "/", "www.google.com"},
		{"healthz", "GET", "/healthz", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pages := &fakePages{}
			h := newTestHandler(pages, NewLimiter(1000, 1000, 10))
			rec := serve(h, tc.method, tc.target, "10.127.0.5:51234", func(r *http.Request) { r.Host = tc.host })
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q", got)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if _, ok := rec.Header()["Server"]; ok {
				t.Error("Server header present")
			}
			if tc.method != "HEAD" && rec.Body.String() != "halaman 10.127.0.5" {
				t.Errorf("body = %q", rec.Body.String())
			}
			if got := pages.asked(); len(got) != 1 || got[0] != "10.127.0.5" {
				t.Errorf("asked = %v", got)
			}
		})
	}
}

func TestForwardedHeadersAreIgnored(t *testing.T) {
	pages := &fakePages{}
	h := newTestHandler(pages, NewLimiter(1000, 1000, 10))
	serve(h, "GET", "/", "10.127.0.5:1", func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "10.127.0.99")
		r.Header.Set("X-Real-IP", "10.127.0.98")
		r.Header.Set("Forwarded", "for=10.127.0.97")
	})
	if got := pages.asked(); len(got) != 1 || got[0] != "10.127.0.5" {
		t.Errorf("asked = %v", got)
	}
}

func TestNonIPv4SourceGetsFallbackWithoutLookup(t *testing.T) {
	for _, remote := range []string{"[2001:db8::1]:1", "garbage", "999.1.1.1:1"} {
		pages := &fakePages{}
		h := newTestHandler(pages, NewLimiter(1000, 1000, 10))
		rec := serve(h, "GET", "/", remote, nil)
		if rec.Code != 200 || rec.Body.String() != string(page.FallbackHTML) {
			t.Errorf("%s: status %d body %q", remote, rec.Code, rec.Body.String())
		}
		if len(pages.asked()) != 0 {
			t.Errorf("%s: Page was called", remote)
		}
	}
}

func TestRateLimitIsPerIP(t *testing.T) {
	pages := &fakePages{}
	h := newTestHandler(pages, NewLimiter(20, 40, 10))
	for i := 1; i <= 40; i++ {
		if rec := serve(h, "GET", "/", "10.127.0.5:1", nil); rec.Code != 200 {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}
	rec := serve(h, "GET", "/", "10.127.0.5:1", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 41: status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "halaman") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("429 body %q headers %v", rec.Body.String(), rec.Header())
	}
	if rec := serve(h, "GET", "/", "10.127.0.6:1", nil); rec.Code != 200 {
		t.Errorf("other IP: status %d", rec.Code)
	}
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "isolir"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestTLSAnswersForAnyServerName(t *testing.T) {
	pages := &fakePages{}
	ts := httptest.NewUnstartedServer(newTestHandler(pages, NewLimiter(1000, 1000, 10)))
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}}
	ts.StartTLS()
	defer ts.Close()

	addr := ts.Listener.Addr().String()
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: "www.google.com", InsecureSkipVerify: true},
	}}
	resp, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "halaman 127.0.0.1" {
		t.Errorf("status %d body %q", resp.StatusCode, body)
	}
}

func TestStatsCountsPerIPAndResets(t *testing.T) {
	s := NewStats()
	s.Record("10.127.0.5")
	s.Record("10.127.0.5")
	s.Record("10.127.0.6")
	got := s.Flush()
	if got["10.127.0.5"] != 2 || got["10.127.0.6"] != 1 || len(got) != 2 {
		t.Errorf("flush = %v", got)
	}
	if again := s.Flush(); len(again) != 0 {
		t.Errorf("not reset: %v", again)
	}
}
