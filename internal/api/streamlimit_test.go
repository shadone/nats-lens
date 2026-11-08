package api

import (
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"testing"
)

func TestStreamListZeroLimitIsBounded(t *testing.T) {
	res := &v1.ListStreamsResponse{Streams: make([]*v1.StreamSummary, 1000), Totals: &v1.StreamTotals{Bytes: 9999}}
	got := LimitStreams(res, 0)
	if len(got.GetStreams()) != 100 {
		t.Fatalf("zero limit returned %d streams; want bounded page of 100", len(got.GetStreams()))
	}
	if got.GetTotalCount() != 1000 || got.GetTotals().GetBytes() != 9999 {
		t.Fatal("page limit changed all-match totals")
	}
}
