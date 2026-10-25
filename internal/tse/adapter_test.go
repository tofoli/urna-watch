package tse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tofoli/urna-watch/internal/domain"
)

const (
	configPath = "/oficial/comum/config/ele-c.json"
	resultPath = "/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json"
)

func testServer(t *testing.T, resultHandler http.HandlerFunc) (*httptest.Server, *int32) {
	t.Helper()
	var configHits int32
	mux := http.NewServeMux()
	mux.HandleFunc(configPath, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&configHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "ele-c.json"))
	})
	mux.HandleFunc(resultPath, resultHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &configHits
}

func newTestAdapter(t *testing.T, base string) *Adapter {
	t.Helper()
	a, err := NewAdapter(Options{BaseURL: base, Environment: "oficial", ConfigTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdapterFetchAndConditional(t *testing.T) {
	body := fixture(t, "br-c0001-e006258-u.json")
	var sawConditional atomic.Bool
	var sawHeaders atomic.Bool
	srv, configHits := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected Accept %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		if r.Header.Get("If-None-Match") == `"etag-1"` {
			sawConditional.Store(true)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		sawHeaders.Store(true)
		w.Header().Set("ETag", `"etag-1"`)
		w.Header().Set("Last-Modified", "Sun, 25 Oct 2026 20:00:00 GMT")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	a := newTestAdapter(t, srv.URL)
	ctx := context.Background()
	r := ref("6258", "1", "br")

	first, err := a.Fetch(ctx, r, Conditional{})
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if first.NotModified || first.Snapshot == nil {
		t.Fatalf("first result = %+v", first)
	}
	if first.ETag != `"etag-1"` || first.LastModified == "" {
		t.Fatalf("validators = %q %q", first.ETag, first.LastModified)
	}
	if !sawHeaders.Load() {
		t.Fatal("result handler not called")
	}

	second, err := a.Fetch(ctx, r, Conditional{ETag: first.ETag, LastModified: first.LastModified})
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if !second.NotModified {
		t.Fatal("expected 304")
	}
	if !sawConditional.Load() {
		t.Fatal("If-None-Match not sent")
	}
	if got := atomic.LoadInt32(configHits); got != 1 {
		t.Fatalf("config fetched %d times, want 1 (cached)", got)
	}
}

func TestAdapterRateLimit(t *testing.T) {
	srv, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false}`))
	})
	a := newTestAdapter(t, srv.URL)
	_, err := a.Fetch(context.Background(), ref("6258", "1", "br"), Conditional{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T %v", err, err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests || httpErr.RetryAfter != 7*time.Second {
		t.Fatalf("error = %+v", httpErr)
	}
	if !httpErr.IsRetryable() {
		t.Fatal("429 should be retryable")
	}
}

func TestAdapterNotFoundIsNotRetryable(t *testing.T) {
	srv, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	a := newTestAdapter(t, srv.URL)
	_, err := a.Fetch(context.Background(), ref("6258", "1", "br"), Conditional{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.IsRetryable() {
		t.Fatal("404 should not be retried inside a cycle")
	}
}

func TestAdapterRejectsInvalidJSON(t *testing.T) {
	srv, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	})
	a := newTestAdapter(t, srv.URL)
	_, err := a.Fetch(context.Background(), ref("6258", "1", "br"), Conditional{})
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ParseError, got %T %v", err, err)
	}
}

func TestAdapterConfigError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(configPath, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	a := newTestAdapter(t, srv.URL)
	_, err := a.Fetch(context.Background(), ref("6258", "1", "br"), Conditional{})
	if err == nil {
		t.Fatal("expected config error")
	}
}

func TestAdapterScopeValidation(t *testing.T) {
	srv, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("result handler should not be called")
	})
	a := newTestAdapter(t, srv.URL)
	r := domain.ElectionRef{ElectionCode: "6260", ContestCode: "3", Scope: domain.Scope{Kind: domain.ScopeState, UF: "sp"}}
	if _, err := a.Fetch(context.Background(), r, Conditional{}); err == nil {
		t.Fatal("expected scope error")
	}
}
