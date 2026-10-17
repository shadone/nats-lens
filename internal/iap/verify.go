// Package iap verifies the signed header Google Identity-Aware Proxy adds
// to every request it lets through, and returns the signed-in user's email.
// The email headers IAP also sets are not signed and are never trusted.
package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Header carries the signed JWT IAP adds to each request.
const Header = "X-Goog-IAP-JWT-Assertion"

// Issuer is the iss claim of an IAP token.
const Issuer = "https://cloud.google.com/iap"

// DefaultSkew is the clock difference accepted on exp and iat.
const DefaultSkew = 30 * time.Second

// ErrInvalid wraps every reason a token is rejected.
var ErrInvalid = errors.New("invalid IAP assertion")

// KeySource returns the public key with a key ID.
type KeySource interface {
	Key(ctx context.Context, kid string) (*ecdsa.PublicKey, error)
}

// Verifier checks IAP assertions for one audience.
type Verifier struct {
	// Audience is the expected aud claim, for example
	// /projects/<number>/global/backendServices/<id>.
	Audience string
	// Keys resolves the key ID in the token header.
	Keys KeySource
	// Now returns the current time; nil means the wall clock.
	Now func() time.Time
	// Skew is the clock difference accepted; zero means DefaultSkew.
	Skew time.Duration
}

type header struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type claims struct {
	Iss   string          `json:"iss"`
	Aud   json.RawMessage `json:"aud"`
	Exp   int64           `json:"exp"`
	Iat   int64           `json:"iat"`
	Email string          `json:"email"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func part(s string, into any) error {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

// Verify checks the token's ES256 signature, issuer, audience, expiry and
// issue time, and returns its email claim.
func (v *Verifier) Verify(ctx context.Context, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", invalid("not a JWT")
	}
	var h header
	if err := part(parts[0], &h); err != nil {
		return "", invalid("header: %v", err)
	}
	if h.Alg != "ES256" {
		return "", invalid("algorithm %q, want ES256", h.Alg)
	}
	key, err := v.Keys.Key(ctx, h.Kid)
	if err != nil {
		return "", fmt.Errorf("%w: key %q: %w", ErrInvalid, h.Kid, err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return "", invalid("signature is not 64 bytes of base64url")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(key, digest[:], r, s) {
		return "", invalid("signature does not verify")
	}
	var c claims
	if err := part(parts[1], &c); err != nil {
		return "", invalid("claims: %v", err)
	}
	if err := v.checkClaims(c); err != nil {
		return "", err
	}
	return c.Email, nil
}

func (v *Verifier) checkClaims(c claims) error {
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	skew := v.Skew
	if skew == 0 {
		skew = DefaultSkew
	}
	switch {
	case c.Iss != Issuer:
		return invalid("issuer %q", c.Iss)
	case !hasAudience(c.Aud, v.Audience):
		return invalid("audience does not match")
	case c.Exp == 0 || !now.Add(-skew).Before(time.Unix(c.Exp, 0)):
		return invalid("expired")
	case c.Iat == 0 || time.Unix(c.Iat, 0).After(now.Add(skew)):
		return invalid("issued in the future")
	case c.Email == "":
		return invalid("no email claim")
	}
	return nil
}

// hasAudience accepts aud as one string or a list of strings.
func hasAudience(raw json.RawMessage, want string) bool {
	if want == "" {
		return false
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == want {
				return true
			}
		}
	}
	return false
}
