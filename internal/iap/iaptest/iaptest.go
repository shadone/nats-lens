// Package iaptest signs IAP-shaped tokens with a local ES256 key for
// tests.
package iaptest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// Signer holds one local key.
type Signer struct {
	Kid     string
	Private *ecdsa.PrivateKey
}

// New returns a signer with a fresh P-256 key.
func New(kid string) *Signer {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return &Signer{Kid: kid, Private: k}
}

// Key implements iap.KeySource for the signer's own key.
func (s *Signer) Key(_ context.Context, kid string) (*ecdsa.PublicKey, error) {
	if kid != s.Kid {
		return nil, errors.New("unknown key ID")
	}
	return &s.Private.PublicKey, nil
}

// Sign returns a JWT with the header fields and claims given, signed with
// ES256 unless alg says otherwise (the signature is then still ES256, so
// that a verifier must reject on the header).
func (s *Signer) Sign(alg string, claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	in := enc(map[string]string{"alg": alg, "kid": s.Kid, "typ": "JWT"}) + "." + enc(claims)
	d := sha256.Sum256([]byte(in))
	r, sv, err := ecdsa.Sign(rand.Reader, s.Private, d[:])
	if err != nil {
		panic(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// JWK returns the public key as a JWK object.
func (s *Signer) JWK() map[string]string {
	b := func(x []byte) string { return base64.RawURLEncoding.EncodeToString(x) }
	pt, err := s.Private.PublicKey.Bytes()
	if err != nil {
		panic(err)
	}
	xb, yb := pt[1:33], pt[33:65]
	return map[string]string{"kid": s.Kid, "kty": "EC", "crv": "P-256", "alg": "ES256", "x": b(xb), "y": b(yb)}
}
