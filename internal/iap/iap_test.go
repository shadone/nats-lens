package iap_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/iap"
	"github.com/shadone/nats-lens/internal/iap/iaptest"
)

const aud = "/projects/123/global/backendServices/456"

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func good() map[string]any {
	return map[string]any{"iss": iap.Issuer, "aud": aud, "exp": now.Add(5 * time.Minute).Unix(),
		"iat": now.Add(-time.Minute).Unix(), "email": "op@example.com", "sub": "accounts.google.com:1"}
}

func with(k string, v any) map[string]any {
	c := good()
	if v == nil {
		delete(c, k)
	} else {
		c[k] = v
	}
	return c
}

func TestVerify(t *testing.T) {
	s := iaptest.New("k1")
	other := iaptest.New("k1")
	v := &iap.Verifier{Audience: aud, Keys: s, Now: func() time.Time { return now }}
	email, err := v.Verify(context.Background(), s.Sign("ES256", good()))
	if err != nil || email != "op@example.com" {
		t.Fatalf("good token: %q, %v", email, err)
	}
	if email, err := v.Verify(context.Background(), s.Sign("ES256", with("aud", []string{"x", aud}))); err != nil || email != "op@example.com" {
		t.Fatalf("aud list: %q, %v", email, err)
	}
	bad := map[string]string{
		"wrong audience":       s.Sign("ES256", with("aud", "/projects/123/global/backendServices/999")),
		"wrong issuer":         s.Sign("ES256", with("iss", "https://accounts.google.com")),
		"expired":              s.Sign("ES256", with("exp", now.Add(-time.Minute).Unix())),
		"no expiry":            s.Sign("ES256", with("exp", nil)),
		"issued in the future": s.Sign("ES256", with("iat", now.Add(5*time.Minute).Unix())),
		"no email":             s.Sign("ES256", with("email", nil)),
		"alg none":             s.Sign("none", good()),
		"alg HS256":            s.Sign("HS256", good()),
		"other key":            other.Sign("ES256", good()),
		"unknown kid":          iaptest.New("k2").Sign("ES256", good()),
		"not a jwt":            "abc.def",
		"empty":                "",
	}
	for name, tok := range bad {
		if _, err := v.Verify(context.Background(), tok); !errors.Is(err, iap.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Within the skew: expired 10 s ago and issued 10 s ahead both pass.
	if _, err := v.Verify(context.Background(), s.Sign("ES256", with("exp", now.Add(-10*time.Second).Unix()))); err != nil {
		t.Errorf("exp within skew: %v", err)
	}
	if _, err := v.Verify(context.Background(), s.Sign("ES256", with("iat", now.Add(10*time.Second).Unix()))); err != nil {
		t.Errorf("iat within skew: %v", err)
	}
	if _, err := (&iap.Verifier{Keys: s, Now: v.Now}).Verify(context.Background(), s.Sign("ES256", good())); err == nil {
		t.Error("a verifier without an audience accepted a token")
	}
}

func TestJWKSCachesAndRefreshes(t *testing.T) {
	k1, k2 := iaptest.New("k1"), iaptest.New("k2")
	var served atomic.Value
	served.Store([]map[string]string{k1.JWK()})
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": served.Load()})
	}))
	defer srv.Close()
	clock := now
	j := &iap.JWKS{URL: srv.URL, Client: srv.Client(), TTL: time.Hour, MinRefresh: time.Minute, Now: func() time.Time { return clock }}
	v := &iap.Verifier{Audience: aud, Keys: j, Now: func() time.Time { return clock }}
	ctx := context.Background()
	for range 3 {
		if _, err := v.Verify(ctx, k1.Sign("ES256", good())); err != nil {
			t.Fatal(err)
		}
	}
	if n := fetches.Load(); n != 1 {
		t.Fatalf("fetched %d times for one key", n)
	}
	// A rotated key: unknown at first, fetched again once MinRefresh passed.
	served.Store([]map[string]string{k1.JWK(), k2.JWK()})
	if _, err := v.Verify(ctx, k2.Sign("ES256", good())); !errors.Is(err, iap.ErrInvalid) {
		t.Fatalf("a new key within MinRefresh of the last fetch: %v", err)
	}
	if n := fetches.Load(); n != 1 {
		t.Fatalf("refetched within MinRefresh? %d fetches", n)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := v.Verify(ctx, k2.Sign("ES256", good())); err != nil {
		t.Fatalf("rotated key: %v", err)
	}
	if n := fetches.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
	if _, err := v.Verify(ctx, iaptest.New("k3").Sign("ES256", good())); !errors.Is(err, iap.ErrInvalid) {
		t.Fatalf("unknown key: %v", err)
	}
	if n := fetches.Load(); n != 2 {
		t.Fatalf("an unknown key within MinRefresh fetched again: %d", n)
	}
}

// A failing key URL is asked once per MinRefresh, not once per token.
func TestFailedFetchIsNotRetriedPerToken(t *testing.T) {
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()
	clock := now
	j := &iap.JWKS{URL: srv.URL, Client: srv.Client(), TTL: time.Hour, MinRefresh: time.Minute, Now: func() time.Time { return clock }}
	v := &iap.Verifier{Audience: aud, Keys: j, Now: func() time.Time { return clock }}
	k := iaptest.New("k1")
	for range 20 {
		if _, err := v.Verify(context.Background(), k.Sign("ES256", good())); err == nil {
			t.Fatal("verified without keys")
		}
	}
	if n := fetches.Load(); n != 1 {
		t.Fatalf("fetches = %d after 20 tokens with the key URL down, want 1", n)
	}
	clock = clock.Add(time.Minute)
	_, _ = v.Verify(context.Background(), k.Sign("ES256", good()))
	if n := fetches.Load(); n != 2 {
		t.Fatalf("fetches = %d after MinRefresh, want 2", n)
	}
}

// A slow key URL holds each caller only until its own context ends, and
// callers that know their key are not held at all.
func TestSlowFetchDoesNotQueueCallers(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
		http.Error(w, "slow", http.StatusInternalServerError)
	}))
	defer srv.Close()
	defer close(release)
	j := &iap.JWKS{URL: srv.URL, Client: srv.Client(), TTL: time.Hour, MinRefresh: time.Minute}
	k := iaptest.New("k1")
	start := time.Now()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			_, _ = (&iap.Verifier{Audience: aud, Keys: j}).Verify(ctx, k.Sign("ES256", good()))
		}()
	}
	wg.Wait()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("4 callers with 300 ms contexts took %v", d)
	}
}

func TestRejectedTokensCarryNoEmail(t *testing.T) {
	k := iaptest.New("k1")
	v := &iap.Verifier{Audience: aud, Keys: k, Now: func() time.Time { return now }}
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, tok := range map[string]string{
		"none":     enc(`{"alg":"none","kid":"k1"}`) + "." + enc(`{}`) + ".",
		"zero sig": enc(`{"alg":"ES256","kid":"k1"}`) + "." + enc(`{}`) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 64)),
		"4 parts":  k.Sign("ES256", good()) + ".x",
		"expired":  k.Sign("ES256", with("exp", now.Add(-time.Minute).Unix())),
		"future":   k.Sign("ES256", with("iat", now.Add(time.Hour).Unix())),
		"aud num":  k.Sign("ES256", with("aud", 5)),
		"aud null": k.Sign("ES256", with("aud", nil)),
		"iss":      k.Sign("ES256", with("iss", "https://accounts.google.com")),
		"long":     strings.Repeat("a", 100000),
	} {
		email, err := v.Verify(context.Background(), tok)
		if err == nil || email != "" {
			t.Errorf("%s: email %q, err %v", name, email, err)
		}
	}
}
