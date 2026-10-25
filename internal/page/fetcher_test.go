package page

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPFetcherSendsTokenAndAddress(t *testing.T) {
	var gotAuth, gotAddr, gotINM, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAddr = r.URL.Query().Get("address")
		gotINM = r.Header.Get("If-None-Match")
		gotPath = r.URL.Path
		w.Header().Set("ETag", `"abc"`)
		_, _ = w.Write([]byte("<html>isolir</html>"))
	}))
	defer srv.Close()

	res, err := NewHTTPFetcher(srv.URL, "tok", srv.Client()).Fetch(context.Background(), "10.127.0.5", `"old"`)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotAddr != "10.127.0.5" || gotINM != `"old"` || gotPath != "/api/internal/isolir/page" {
		t.Fatalf("request = %q %q %q %q", gotAuth, gotAddr, gotINM, gotPath)
	}
	if string(res.HTML) != "<html>isolir</html>" || res.ETag != `"abc"` || res.NotModified {
		t.Fatalf("result = %+v", res)
	}
}

func TestHTTPFetcherNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	res, err := NewHTTPFetcher(srv.URL, "tok", srv.Client()).Fetch(context.Background(), "10.127.0.5", `"abc"`)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NotModified {
		t.Fatalf("result = %+v", res)
	}
}

func TestHTTPFetcherErrors(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"unauthorized": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		},
		"body over 1 MiB": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", 2<<20)))
		},
	}
	for name, h := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			if _, err := NewHTTPFetcher(srv.URL, "tok", srv.Client()).Fetch(context.Background(), "10.127.0.5", ""); err == nil {
				t.Fatal("want error")
			}
		})
	}
}
