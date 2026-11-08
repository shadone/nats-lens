package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func listTestRange() *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(time.Unix(1, 0)), End: timestamppb.New(time.Unix(100, 0))}
}
func TestEntityPagesTraversalAndReset(t *testing.T) {
	var pages EntityPages
	calls := 0
	load := func() (*EntitySnapshot, error) {
		calls++
		s := &EntitySnapshot{Range: listTestRange(), Epoch: 7}
		for i := range 201 {
			s.Streams = append(s.Streams, &v1.StreamSummary{Entity: &v1.EntityRef{Key: fmt.Sprint(i)}})
		}
		return s, nil
	}
	s, offset, err := pages.Begin(context.Background(), "", "scope", load)
	if err != nil || offset != 0 {
		t.Fatal(offset, err)
	}
	token := s.Next(100)
	s2, offset, err := pages.Begin(context.Background(), token, "scope", load)
	if err != nil || offset != 100 || s2 != s || calls != 1 {
		t.Fatal(offset, calls, err)
	}
	if s.Next(201) != "" {
		t.Fatal("last page has continuation")
	}
	if _, _, err = pages.Begin(context.Background(), token, "changed-filter", load); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	r, err := EntityListRange(nil, token, time.Now())
	if err != nil || !r.End.AsTime().Equal(s.Range.End.AsTime()) {
		t.Fatal(r, err)
	}
	s.created = time.Now().Add(-ListContextLifetime)
	if _, _, err = pages.Begin(context.Background(), token, "scope", load); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatal(err)
	}
}
func TestEntityPagesLimitsCancellationAndEviction(t *testing.T) {
	var pages EntityPages
	load := func() (*EntitySnapshot, error) {
		return &EntitySnapshot{Range: listTestRange(), Consumers: []*v1.ConsumerSummary{{}, {}}}, nil
	}
	s, _, err := pages.Begin(context.Background(), "", "scope", load)
	if err != nil {
		t.Fatal(err)
	}
	for range MaxListContexts {
		if _, _, err = pages.Begin(context.Background(), "", "scope", load); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = pages.Begin(context.Background(), s.Next(1), "scope", load); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = pages.Begin(ctx, "", "scope", func() (*EntitySnapshot, error) { t.Fatal("canceled read reached loader"); return nil, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err = pages.Begin(context.Background(), "garbage", "scope", load); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err = pages.Begin(context.Background(), "", "scope", func() (*EntitySnapshot, error) {
		return &EntitySnapshot{Streams: []*v1.StreamSummary{{Leader: strings.Repeat("x", MaxListContextBytes+1)}}}, nil
	}); !errors.Is(err, ErrResourceExhausted) {
		t.Fatal(err)
	}
	if FitsEntityPage(&v1.ListStreamsResponse{Streams: []*v1.StreamSummary{{Leader: strings.Repeat("x", MaxEntityPageBytes)}}}) {
		t.Fatal("oversized entity fits")
	}
	if _, err = EntityLimit(-1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if n, _ := EntityLimit(9999); n != 100 {
		t.Fatal(n)
	}
}
func TestEntityPagesConcurrentReaders(t *testing.T) {
	var pages EntityPages
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			_, _, err := pages.Begin(context.Background(), "", "scope", func() (*EntitySnapshot, error) { return &EntitySnapshot{Range: listTestRange()}, nil })
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(pages.contexts) > MaxListContexts {
		t.Fatal(len(pages.contexts))
	}
}

func TestEntityScopeSeparatesRPCs(t *testing.T) {
	if EntityScope(&v1.ListStreamsRequest{Range: listTestRange()}) == EntityScope(&v1.ListConsumersRequest{Range: listTestRange()}) {
		t.Fatal("cross-RPC cursor scopes collide")
	}
}
