// Package page obtains the isolir page for a customer address from netmanager
// and caches it, so a slow or unreachable netmanager never blocks or breaks
// the answer given to a suspended customer.
package page

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxBodyBytes caps what is read from netmanager; the page is small HTML, so
// anything bigger is a misbehaving upstream.
const maxBodyBytes = 1 << 20

// Result is netmanager's answer. HTML and ETag are empty when NotModified.
type Result struct {
	HTML        []byte
	ETag        string
	NotModified bool
}

// Fetcher asks netmanager for the page of one customer address. etag is the
// validator of the copy the caller holds, or empty.
type Fetcher interface {
	Fetch(ctx context.Context, address, etag string) (Result, error)
}

type httpFetcher struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPFetcher returns a Fetcher that calls netmanager's internal isolir
// endpoint at baseURL, authenticating with token.
func NewHTTPFetcher(baseURL, token string, client *http.Client) Fetcher {
	return &httpFetcher{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: client}
}

func (f *httpFetcher) Fetch(ctx context.Context, address, etag string) (Result, error) {
	u := f.baseURL + "/api/internal/isolir/page?address=" + url.QueryEscape(address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return Result{NotModified: true}, nil
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
		if err != nil {
			return Result{}, err
		}
		if len(body) > maxBodyBytes {
			return Result{}, fmt.Errorf("netmanager page larger than %d bytes", maxBodyBytes)
		}
		// A blank page cached for ttl would replace the last good copy.
		if len(body) == 0 {
			return Result{}, errors.New("netmanager answered 200 with an empty page")
		}
		return Result{HTML: body, ETag: resp.Header.Get("ETag")}, nil
	default:
		return Result{}, fmt.Errorf("netmanager answered %d", resp.StatusCode)
	}
}
