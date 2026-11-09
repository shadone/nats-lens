package api

import (
	"errors"
	"strings"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

func TestEpisodeCursorKeepsFullTimestampPrecision(t *testing.T) {
	var pages EpisodePages
	now := time.Date(2026, 10, 17, 14, 30, 0, 123456789, time.UTC)
	first := &v1.ListFindingEpisodesRequest{System: "shop"}
	c, err := pages.Prepare(first, now)
	if err != nil {
		t.Fatal(err)
	}
	next := &v1.ListFindingEpisodesRequest{System: "shop", PageToken: c.Next(9007199254740993, 3, 1)}
	got, err := pages.Prepare(next, now.Add(time.Minute))
	if err != nil || !proto.Equal(first.Range, next.Range) || got.ReadEpoch != 9007199254740993 {
		t.Fatalf("cursor lost timestamp/epoch precision: %v %v %v", first.Range, next.Range, err)
	}
}

func TestEpisodeCursorSourceScopeAndExpiry(t *testing.T) {
	var pages EpisodePages
	if err := pages.InitImmutable(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	first := &v1.ListFindingEpisodesRequest{System: "shop"}
	c, err := pages.Prepare(first, now)
	if err != nil {
		t.Fatal(err)
	}
	token := c.Next(9007199254740993, 3, 1)
	var reopened EpisodePages
	if err := reopened.InitImmutable(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Prepare(&v1.ListFindingEpisodesRequest{System: "shop", PageToken: token}, now); err != nil {
		t.Fatalf("same closed source cannot reopen: %v", err)
	}
	var other EpisodePages
	if err := other.InitImmutable(strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Prepare(&v1.ListFindingEpisodesRequest{System: "shop", PageToken: token}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other source accepted: %v", err)
	}
	if _, err := pages.Prepare(&v1.ListFindingEpisodesRequest{System: "other", PageToken: token}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other system accepted: %v", err)
	}
	c.Issued = time.Now().Add(-ListContextLifetime - time.Minute).Unix()
	if _, err := pages.Prepare(&v1.ListFindingEpisodesRequest{System: "shop", PageToken: c.Next(3, 3, 1)}, now); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("expired cursor accepted: %v", err)
	}
}
