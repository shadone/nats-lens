package storeprov_test

import (
	"errors"
	"github.com/shadone/nats-lens/internal/api"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func episodeProvider(t *testing.T) *storeprov.Provider {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	for e := int64(1); e <= 3; e++ {
		r.epoch(e, t0.Add(time.Duration(e-1)*time.Minute), "S0")
		var findings []store.FindingRow
		if e != 2 {
			sev, detail := "warning", "first"
			if e == 3 {
				sev, detail = "critical", "recurrence"
			}
			findings = []store.FindingRow{{EntityKey: "APP/orders", Severity: sev, Detail: detail}}
		}
		if _, err := st.ApplyCheckRuns(ctx, e, []store.CheckRun{{Code: "stream-001", EntityType: "stream", Status: store.RunOK, Rows: findings}}); err != nil {
			t.Fatal(err)
		}
	}
	return &storeprov.Provider{St: st, Interval: time.Minute, Clock: func() time.Time { return t0.Add(3 * time.Minute) }}
}

func TestFindingEpisodeAPIKeepsSelectedOccurrence(t *testing.T) {
	p := episodeProvider(t)
	req := &v1.ListFindingEpisodesRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(30 * time.Second))}, FindingId: "stream-001:APP/orders", FirstRetainedEpoch: 1, IncludeTransitions: true}
	got, err := p.ListFindingEpisodes(ctx, req)
	if err != nil || len(got.GetEpisodes()) != 1 {
		t.Fatalf("episode = %v, %v", got, err)
	}
	e := got.Episodes[0]
	if e.GetFinding().GetDetail() != "first" || e.GetFinding().GetState() != v1.FindingState_FINDING_STATE_FIRING || e.GetLatestRetainedSeverity() != v1.Severity_SEVERITY_CRITICAL || e.GetStartEpoch() != 1 || e.GetFirstRetainedEpoch() != 1 {
		t.Fatalf("historical/current occurrence conflated: %v", e)
	}
	if e.GetObservedResolutionAt() == nil || !e.GetObservedResolutionAt().AsTime().Equal(t0.Add(time.Minute)) {
		t.Fatalf("resolution evidence lost: %v", e)
	}
	if len(e.Transitions) != 2 || e.Transitions[0].GetEpoch() != 2 || e.Transitions[1].GetEpoch() != 1 || e.TransitionCount != 2 {
		t.Fatalf("raw history not scoped to occurrence: %v", e)
	}
	if e.GetFinding().GetConditionSince() != nil || len(e.GetFinding().GetRelatedConfig()) != 0 || len(e.GetUnavailable()) == 0 {
		t.Fatalf("historical metadata invented: %v", e)
	}
}

func TestFindingEpisodeAPIPagingFreezesMembershipAndScope(t *testing.T) {
	p := episodeProvider(t)
	req := &v1.ListFindingEpisodesRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(10 * time.Minute))}, PageSize: 1}
	first, err := p.ListFindingEpisodes(ctx, req)
	if err != nil || first.GetTotalCount() != 2 || len(first.GetEpisodes()) != 1 || first.GetNextPageToken() == "" || first.GetReadEpoch() != 3 {
		t.Fatalf("first = %v, %v", first, err)
	}
	r := rows{t, p.St}
	for e := int64(4); e <= 5; e++ {
		r.epoch(e, t0.Add(time.Duration(e-1)*time.Minute), "S0")
		var findings []store.FindingRow
		if e == 5 {
			findings = []store.FindingRow{{EntityKey: "APP/orders", Severity: "info", Detail: "later"}}
		}
		if _, err := p.St.ApplyCheckRuns(ctx, e, []store.CheckRun{{Code: "stream-001", EntityType: "stream", Status: store.RunOK, Rows: findings}}); err != nil {
			t.Fatal(err)
		}
	}
	req.PageToken = first.NextPageToken
	next, err := p.ListFindingEpisodes(ctx, req)
	if err != nil || next.GetTotalCount() != 2 || len(next.GetEpisodes()) != 1 || next.Episodes[0].GetFirstRetainedEpoch() != 1 {
		t.Fatalf("continuation shifted: %v, %v", next, err)
	}
	req.CheckCode = "stream-002"
	if _, err := p.ListFindingEpisodes(ctx, req); err == nil {
		t.Fatal("changed scope accepted frozen cursor")
	}
}

func TestFindingEpisodeAPIRetentionRequiresTraversalReset(t *testing.T) {
	p := episodeProvider(t)
	req := &v1.ListFindingEpisodesRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(10 * time.Minute))}, PageSize: 1}
	first, err := p.ListFindingEpisodes(ctx, req)
	if err != nil || first.GetNextPageToken() == "" {
		t.Fatalf("first: %v %v", first, err)
	}
	// Removing the old firing leaves a resolution fragment: the occurrence
	// count stays two, so a count-only guard cannot detect substitution.
	if _, err := p.St.DB().Exec(`DELETE FROM finding_transitions WHERE epoch=1`); err != nil {
		t.Fatal(err)
	}
	req.PageToken = first.NextPageToken
	if _, err := p.ListFindingEpisodes(ctx, req); !errors.Is(err, api.ErrFailedPrecondition) {
		t.Fatalf("retention changed identities without reset: %v", err)
	}
}
