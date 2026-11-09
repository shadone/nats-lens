package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

func TestImmutableEntityPagesRehydrateAndValidate(t *testing.T) {
	ctx := context.Background()
	fingerprint := strings.Repeat("a", 64)
	newPager := func() *EntityPages {
		p := new(EntityPages)
		if err := p.InitImmutable(fingerprint); err != nil {
			t.Fatal(err)
		}
		return p
	}
	load := func() (*EntitySnapshot, error) {
		return &EntitySnapshot{Range: listTestRange(), Epoch: 9007199254740993, Streams: []*v1.StreamSummary{{Entity: &v1.EntityRef{Key: "APP/orders"}, Bytes: proto.Int64(9007199254740993)}, {Entity: &v1.EntityRef{Key: "APP/payments"}, Bytes: proto.Int64(10)}}}, nil
	}
	first, _, err := newPager().Begin(ctx, "", "scope", load)
	if err != nil {
		t.Fatal(err)
	}
	token := first.Next(1)
	second, offset, err := newPager().Begin(ctx, token, "scope", load)
	if err != nil || offset != 1 || first.id != second.id {
		t.Fatalf("rehydrate %d %v", offset, err)
	}
	if second.Next(2) != "" {
		t.Fatal("final page continued")
	}
	parsed, err := readEntityToken(token)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(v entityToken) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	for _, c := range []struct {
		name   string
		mutate func(*entityToken)
	}{
		{"expired", func(v *entityToken) { v.IssuedAt = time.Now().Add(-ListContextLifetime - time.Second).Unix() }},
		{"future", func(v *entityToken) { v.IssuedAt = time.Now().Add(time.Minute).Unix() }},
		{"missing_time", func(v *entityToken) { v.IssuedAt = 0 }},
		{"negative_offset", func(v *entityToken) { v.Offset = -1 }},
		{"past_end", func(v *entityToken) { v.Offset = 2 }},
		{"huge_offset", func(v *entityToken) { v.Offset = MaxOffset + 1 }},
		{"invalid_id", func(v *entityToken) { v.ID = "bad" }},
		{"wrong_hash", func(v *entityToken) { v.ID = strings.Repeat("z", 22) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := parsed
			c.mutate(&v)
			if _, _, err := newPager().Begin(ctx, encode(v), "scope", load); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	for _, scope := range []string{"changed filter", "changed system", "changed range", "changed RPC"} {
		if _, _, err := newPager().Begin(ctx, token, scope, load); !errors.Is(err, ErrInvalid) {
			t.Fatalf("scope: %v", err)
		}
	}
	changed := newPager()
	changed.fingerprint = strings.Repeat("b", 64)
	if _, _, err = changed.Begin(ctx, token, "scope", load); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed dataset: %v", err)
	}
	if _, _, err = newPager().Begin(ctx, token, "scope", func() (*EntitySnapshot, error) { s, _ := load(); *s.Streams[0].Bytes++; return s, nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed summary: %v", err)
	}
	if _, _, err = newPager().Begin(ctx, strings.Repeat("a", 1025), "scope", load); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err = newPager().Begin(cancelled, token, "scope", func() (*EntitySnapshot, error) { t.Fatal("cancel reached loader"); return nil, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err = newPager().Begin(ctx, token, "scope", func() (*EntitySnapshot, error) {
		return &EntitySnapshot{Streams: make([]*v1.StreamSummary, MaxListRankingRows+1)}, nil
	}); !errors.Is(err, ErrResourceExhausted) {
		t.Fatal(err)
	}
	if _, _, err = newPager().Begin(ctx, token, "scope", func() (*EntitySnapshot, error) {
		return &EntitySnapshot{Streams: []*v1.StreamSummary{{Leader: strings.Repeat("x", MaxListContextBytes+1)}}}, nil
	}); !errors.Is(err, ErrResourceExhausted) {
		t.Fatal(err)
	}
	for _, bad := range []string{"", strings.Repeat("A", 64), strings.Repeat("g", 64), "abcd"} {
		if err := new(EntityPages).InitImmutable(bad); err == nil {
			t.Fatal("bad fingerprint accepted")
		}
	}
	pager := newPager()
	if err := pager.InitImmutable(fingerprint); err == nil {
		t.Fatal("reinitialization accepted")
	}
}
