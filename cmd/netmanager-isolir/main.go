// Command netmanager-isolir serves the isolir page to suspended customers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hadiahterlupakan/netmanager-isolir/internal/page"
	"github.com/hadiahterlupakan/netmanager-isolir/internal/server"
)

const (
	httpAddr   = ":8080"
	httpsAddr  = ":8443"
	healthAddr = ":8081"

	ratePerSec    = 20
	burst         = 40
	maxConnsPerIP = 32

	cacheTTL      = 60 * time.Second
	cacheStaleFor = 24 * time.Hour
	fetchTimeout  = 3 * time.Second
	clientTimeout = 5 * time.Second

	statsInterval   = 5 * time.Minute
	shutdownTimeout = 5 * time.Second
)

type config struct {
	netmanagerURL, netmanagerToken, certFile, keyFile string
}

func loadConfig(getenv func(string) string) (config, error) {
	var cfg config
	for _, f := range []struct {
		name string
		dst  *string
	}{
		{"NETMANAGER_URL", &cfg.netmanagerURL},
		{"NETMANAGER_TOKEN", &cfg.netmanagerToken},
		{"TLS_CERT_FILE", &cfg.certFile},
		{"TLS_KEY_FILE", &cfg.keyFile},
	} {
		if *f.dst = getenv(f.name); *f.dst == "" {
			return config{}, fmt.Errorf("variabel lingkungan %s wajib diisi", f.name)
		}
	}
	return cfg, nil
}

func newCustomerServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8192,
	}
}

func healthServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	// The service must reach netmanager and nothing else, so a redirect is an
	// answer, never a place to go.
	client := &http.Client{
		Timeout:       clientTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	cache := page.NewCache(page.NewHTTPFetcher(cfg.netmanagerURL, cfg.netmanagerToken, client),
		cacheTTL, cacheStaleFor, fetchTimeout, time.Now)
	limiter := server.NewLimiter(ratePerSec, burst, maxConnsPerIP)
	stats := server.NewStats()
	handler := server.NewHandler(cache, limiter, stats)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go stats.LogEvery(ctx, statsInterval)

	httpSrv, httpsSrv, healthSrv := newCustomerServer(handler), newCustomerServer(handler), healthServer()
	errc := make(chan error, 3)
	go func() { errc <- serve(httpSrv, httpAddr, limiter, "", "") }()
	go func() { errc <- serve(httpsSrv, httpsAddr, limiter, cfg.certFile, cfg.keyFile) }()
	go func() { errc <- serve(healthSrv, healthAddr, nil, "", "") }()

	select {
	case err := <-errc:
		log.Fatal(err)
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, s := range []*http.Server{httpSrv, httpsSrv, healthSrv} {
		s.Shutdown(shutCtx)
	}
}

func serve(s *http.Server, addr string, limiter *server.Limiter, certFile, keyFile string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if limiter != nil {
		ln = limiter.WrapListener(ln)
	}
	if certFile != "" {
		err = s.ServeTLS(ln, certFile, keyFile)
	} else {
		err = s.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
