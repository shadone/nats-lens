package fake

import (
	"context"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestDemoFindingEpisodesExposeRepeatedRecordedOccurrences(t *testing.T) {
	p := New(false)
	rng := &v1.TimeRange{Start: ts(FixedNow.Add(-24 * time.Hour)), End: ts(FixedNow)}
	timeline, err := p.GetStatusTimeline(context.Background(), &v1.GetStatusTimelineRequest{Range: rng, Bucket: durationpb.New(15 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.GetFindingReadEpoch() == 0 {
		t.Error("demo Timeline has no stable finding boundary")
	}
	got, err := p.ListFindingEpisodes(context.Background(), &v1.ListFindingEpisodesRequest{Range: rng, CheckCode: "consumer-002", ReadEpoch: proto.Int64(timeline.GetFindingReadEpoch())})
	if err != nil || got.GetTotalCount() != 5 || len(got.GetEpisodes()) != 5 {
		t.Fatalf("demo collapsed past occurrences: %v %v", got, err)
	}
	seen := map[int64]bool{}
	for _, e := range got.Episodes {
		if seen[e.FirstRetainedEpoch] || e.GetFinding().GetCheckCode() != "consumer-002" || e.GetFirstRetainedEpoch() == 0 {
			t.Fatalf("unstable demo occurrence: %v", e)
		}
		seen[e.FirstRetainedEpoch] = true
	}
}

func TestDemoFindingEpisodePinsSurviveLiveClockAdvance(t *testing.T) {
	p := New(true)
	now := FixedNow
	p.clock = func() time.Time { return now }
	rng := &v1.TimeRange{Start: ts(FixedNow.Add(-24 * time.Hour)), End: ts(FixedNow)}
	timeline, err := p.GetStatusTimeline(context.Background(), &v1.GetStatusTimelineRequest{Range: rng, Bucket: durationpb.New(15 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	all, err := p.ListFindingEpisodes(context.Background(), &v1.ListFindingEpisodesRequest{Range: rng, CheckCode: "consumer-002", ReadEpoch: proto.Int64(timeline.GetFindingReadEpoch())})
	if err != nil || len(all.GetEpisodes()) != 5 {
		t.Fatalf("demo first: %v %v", all, err)
	}
	old := all.Episodes[len(all.Episodes)-1]
	now = now.Add(time.Hour)
	selected, err := p.ListFindingEpisodes(context.Background(), &v1.ListFindingEpisodesRequest{Range: rng, FindingId: old.Finding.Id, FirstRetainedEpoch: old.FirstRetainedEpoch, IncludeTransitions: true})
	if err != nil || len(selected.GetEpisodes()) != 1 || !proto.Equal(selected.Episodes[0].Finding.Since, old.Finding.Since) || !proto.Equal(selected.Episodes[0].ObservedResolutionAt, old.ObservedResolutionAt) || len(selected.Episodes[0].Transitions) != 2 {
		t.Fatalf("live demo pin moved: %v %v", selected, err)
	}
	first, err := p.ListFindingEpisodes(context.Background(), &v1.ListFindingEpisodesRequest{Range: rng, CheckCode: "consumer-002", ReadEpoch: proto.Int64(timeline.GetFindingReadEpoch()), PageSize: 1})
	if err != nil || first.GetNextPageToken() == "" {
		t.Fatalf("page first: %v %v", first, err)
	}
	now = now.Add(time.Minute)
	next, err := p.ListFindingEpisodes(context.Background(), &v1.ListFindingEpisodesRequest{CheckCode: "consumer-002", PageSize: 1, PageToken: first.NextPageToken})
	if err != nil || len(next.GetEpisodes()) != 1 || next.Episodes[0].FirstRetainedEpoch == first.Episodes[0].FirstRetainedEpoch || !proto.Equal(next.Range, first.Range) {
		t.Fatalf("clock changed traversal: %v %v", next, err)
	}
}
