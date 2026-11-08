package investigation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteTrustAuthAndPrecision(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-only" {
			t.Error("missing credential")
		}
		if !strings.HasPrefix(r.URL.Path, Path+"/"+Service+"/") {
			t.Error(r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "GetLensInfo"):
			w.Write([]byte(`{"version":"fixture","now":"2026-10-17T14:30:00Z","system":"shop"}`))
		default:
			w.Write([]byte(`{"streams":[{"bytes":"9007199254740993"}]}`))
		}
	}))
	defer srv.Close()
	p := Profile{Deployment: srv.URL, Endpoint: srv.URL, TokenEnv: "NATSLENS_FIXTURE_TOKEN"}
	t.Setenv(p.TokenEnv, "fixture-only")
	src, err := NewRemote(p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := src.Query(context.Background(), "ListStreams", []byte(`{"system":"shop"}`))
	if err != nil || !strings.Contains(string(out), `"9007199254740993"`) {
		t.Fatalf("%s %v", out, err)
	}
	before := calls
	if _, err = src.Query(context.Background(), "MuteFinding", []byte(`{}`)); err == nil || calls != before {
		t.Fatal("write dispatched")
	}
	for _, endpoint := range []string{"http://example.com", "https://user:secret@example.com", "https://lens.example.com/?token=secret"} {
		if _, err := NewRemote(Profile{Deployment: "https://lens.example.com", Endpoint: endpoint}); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
}
func TestRemoteNoRedirectAndBoundedRead(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	src, _ := NewRemote(Profile{Deployment: srv.URL, Endpoint: srv.URL})
	if _, err := src.Query(context.Background(), "ListMetrics", []byte(`{}`)); err == nil || leaked {
		t.Fatalf("redirect followed: %v", err)
	}
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"metrics":[]}` + strings.Repeat(" ", MaxResponseBytes)))
	}))
	defer huge.Close()
	src, _ = NewRemote(Profile{Deployment: huge.URL, Endpoint: huge.URL})
	if _, err := src.Query(context.Background(), "ListMetrics", []byte(`{}`)); err == nil {
		t.Fatal("unbounded response")
	}
}
func TestRemoteCancellationAndAuthFailure(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			w.Write([]byte("private auth detail"))
			return
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	src, _ := NewRemote(Profile{Deployment: srv.URL, Endpoint: srv.URL})
	_, err := src.Query(context.Background(), "ListMetrics", []byte(`{}`))
	if err == nil || strings.Contains(err.Error(), "private auth detail") {
		t.Fatal(err)
	}
	t.Setenv("NATSLENS_CANCEL_TOKEN", "fixture")
	src, _ = NewRemote(Profile{Deployment: srv.URL, Endpoint: srv.URL, TokenEnv: "NATSLENS_CANCEL_TOKEN"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = src.Query(ctx, "ListMetrics", []byte(`{}`)); err == nil {
		t.Fatal("cancellation ignored")
	}
}
