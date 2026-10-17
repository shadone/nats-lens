package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// JWKURL publishes the keys IAP signs with.
const JWKURL = "https://www.gstatic.com/iap/verify/public_key-jwk"

// JWKS fetches the IAP keys and caches them. The key set is fetched again
// after TTL, and early when a token names an unknown key. Fetches, failed
// ones included, start at most once per MinRefresh, so that bad tokens or
// a failing key URL cannot make it hammer the URL. One fetch runs at a
// time, outside the lock, with its own timeout; callers that know their key
// keep using the cached set meanwhile, and the cached set stays in use when
// refreshes fail.
type JWKS struct {
	URL        string
	Client     *http.Client
	TTL        time.Duration
	MinRefresh time.Duration
	// Now returns the current time; nil means the wall clock.
	Now func() time.Time

	mu        sync.Mutex
	keys      map[string]*ecdsa.PublicKey
	fetched   time.Time     // last successful fetch
	attempted time.Time     // last fetch started, successful or not
	lastErr   error         // error of the last fetch, nil after a success
	inflight  chan struct{} // closed when the running fetch ends; nil when none runs
}

// NewJWKS returns a key source for Google's IAP keys: refreshed every
// hour, and at most once a minute for an unknown key.
func NewJWKS() *JWKS {
	return &JWKS{URL: JWKURL, Client: &http.Client{Timeout: 10 * time.Second}, TTL: time.Hour, MinRefresh: time.Minute}
}

// ErrUnknownKey is returned for a key ID the key set does not hold.
var ErrUnknownKey = errors.New("unknown key ID")

func (j *JWKS) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// Key returns the key with the ID. A known key is returned from the cache
// at once, starting a refresh in the background when the set is older than
// TTL. An unknown key waits for a fetch, or for ctx, when one may start or
// is running.
func (j *JWKS) Key(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	j.mu.Lock()
	now := j.now()
	key, ok := j.keys[kid]
	due := j.keys == nil || now.Sub(j.fetched) >= j.TTL || !ok
	if due && j.inflight == nil && (j.attempted.IsZero() || now.Sub(j.attempted) >= j.MinRefresh) {
		j.startFetch(now)
	}
	wait := j.inflight
	j.mu.Unlock()
	if ok {
		return key, nil
	}
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if key, ok := j.keys[kid]; ok {
		return key, nil
	}
	if j.keys == nil && j.lastErr != nil {
		return nil, j.lastErr
	}
	return nil, ErrUnknownKey
}

// startFetch runs one fetch in the background; j.mu is held.
func (j *JWKS) startFetch(now time.Time) {
	done := make(chan struct{})
	j.inflight, j.attempted = done, now
	go func() {
		keys, err := j.fetch(context.Background())
		j.mu.Lock()
		if err == nil {
			j.keys, j.fetched = keys, now
		}
		j.lastErr, j.inflight = err, nil
		j.mu.Unlock()
		close(done)
	}()
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (j *JWKS) fetch(ctx context.Context) (map[string]*ecdsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return nil, err
	}
	res, err := j.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch IAP keys: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch IAP keys: HTTP %d", res.StatusCode)
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&set); err != nil {
		return nil, fmt.Errorf("decode IAP keys: %w", err)
	}
	out := map[string]*ecdsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "EC" || k.Crv != "P-256" {
			continue
		}
		pub, err := ParseJWK(k.X, k.Y)
		if err != nil {
			return nil, fmt.Errorf("IAP key %s: %w", k.Kid, err)
		}
		out[k.Kid] = pub
	}
	return out, nil
}

// ParseJWK builds a P-256 public key from a JWK's base64url x and y.
func ParseJWK(x, y string) (*ecdsa.PublicKey, error) {
	xb, err := base64.RawURLEncoding.DecodeString(x)
	if err != nil {
		return nil, err
	}
	yb, err := base64.RawURLEncoding.DecodeString(y)
	if err != nil {
		return nil, err
	}
	if len(xb) != 32 || len(yb) != 32 {
		return nil, errors.New("coordinates are not 32 bytes")
	}
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, xb...), yb...))
}
