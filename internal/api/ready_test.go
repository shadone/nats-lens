package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWithReady(t *testing.T) {
	var ready atomic.Bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := WithReady(next, ready.Load)
	code := func(method, path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}
	if c := code(http.MethodGet, ReadyPath); c != http.StatusServiceUnavailable {
		t.Fatalf("before ready: %d", c)
	}
	ready.Store(true)
	if c := code(http.MethodGet, ReadyPath); c != http.StatusOK {
		t.Fatalf("ready: %d", c)
	}
	if c := code(http.MethodPost, ReadyPath); c != http.StatusMethodNotAllowed {
		t.Fatalf("post: %d", c)
	}
	if c := code(http.MethodGet, "/healthz"); c != http.StatusTeapot {
		t.Fatalf("other paths go to next: %d", c)
	}
	if c := (func() int {
		rec := httptest.NewRecorder()
		WithReady(next, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ReadyPath, nil))
		return rec.Code
	})(); c != http.StatusOK {
		t.Fatalf("nil ready: %d", c)
	}
}
