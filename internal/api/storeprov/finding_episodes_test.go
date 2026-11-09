package storeprov_test

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
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

func TestFindingEpisodeHTTPContinuationKeepsOriginalRange(t *testing.T) {
	p := episodeProvider(t)
	now := p.Now()
	p.Clock = func() time.Time { return now }
	_, client := apitest.ServeHandler(t, &api.Handler{Systems: api.One("shop", "Example shop", p)})
	first, err := client.ListFindingEpisodes(ctx, connect.NewRequest(&v1.ListFindingEpisodesRequest{System: "shop", PageSize: 1}))
	if err != nil || first.Msg.GetNextPageToken() == "" {
		t.Fatalf("first HTTP page: %v %v", first, err)
	}
	now = now.Add(time.Minute)
	next, err := client.ListFindingEpisodes(ctx, connect.NewRequest(&v1.ListFindingEpisodesRequest{System: "shop", PageSize: 1, PageToken: first.Msg.NextPageToken}))
	if err != nil {
		t.Fatalf("omitted continuation range moved: %v", err)
	}
	if !proto.Equal(first.Msg.Range, next.Msg.Range) || len(next.Msg.Episodes) != 1 || next.Msg.Episodes[0].FirstRetainedEpoch != 1 {
		t.Fatalf("HTTP continuation: %v", next.Msg)
	}
}

func TestFindingEpisodeAPIMissingHistoryIsUnavailable(t *testing.T) {
	p := episodeProvider(t)
	got, err := p.ListFindingEpisodes(ctx, &v1.ListFindingEpisodesRequest{FindingId: "stream-001:APP/orders", FirstRetainedEpoch: 99})
	if err != nil || len(got.GetEpisodes()) != 0 || len(got.GetUnavailable()) == 0 {
		t.Fatalf("missing occurrence not explained: %v %v", got, err)
	}
}

func TestFindingEpisodeAPIRawHistoryPagesOnlySelectedOccurrence(t *testing.T) {
	st := emptyStore(t)
	_, err := st.DB().Exec(`INSERT INTO finding_transitions(finding_id,check_code,entity_type,entity_key,epoch,time,kind,severity,detail)
 SELECT 'stream-001:APP/orders','stream-001','stream','APP/orders',n+1,make_timestamp(?::BIGINT)+n*INTERVAL 1 SECOND,
 CASE WHEN n=0 THEN 'firing' ELSE 'changed' END,CASE WHEN n%2=0 THEN 'warning' ELSE 'critical' END,'Recorded transition'
 FROM range(130) t(n)`, t0.UnixMicro())
	if err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Clock: func() time.Time { return t0.Add(3 * time.Minute) }}
	req := &v1.ListFindingEpisodesRequest{FindingId: "stream-001:APP/orders", FirstRetainedEpoch: 1, IncludeTransitions: true}
	first, err := p.ListFindingEpisodes(ctx, req)
	if err != nil || len(first.GetEpisodes()) != 1 {
		t.Fatalf("raw first: %v %v", first, err)
	}
	e := first.Episodes[0]
	if e.TransitionCount != 130 || len(e.Transitions) != 100 || e.NextTransitionOffset != 100 || e.TransitionPageToken == "" {
		t.Fatalf("raw history not bounded: %v", e)
	}
	req.TransitionOffset = e.NextTransitionOffset
	req.PageToken = e.TransitionPageToken
	next, err := p.ListFindingEpisodes(ctx, req)
	if err != nil || len(next.GetEpisodes()) != 1 || len(next.Episodes[0].Transitions) != 30 || next.Episodes[0].NextTransitionOffset != 0 {
		t.Fatalf("raw continuation: %v %v", next, err)
	}
	seen := map[int64]bool{}
	for _, tr := range append(e.Transitions, next.Episodes[0].Transitions...) {
		if seen[tr.Epoch] {
			t.Fatal("duplicate transition", tr.Epoch)
		}
		seen[tr.Epoch] = true
	}
	if len(seen) != 130 || !seen[1] || !seen[130] {
		t.Fatalf("raw coverage: %d", len(seen))
	}
	if next.Episodes[0].LatestRetainedState != v1.FindingState_FINDING_STATE_UNSPECIFIED {
		t.Fatal("missing current finding row invented present state")
	}
}

func TestTimelineOccurrenceBoundaryExcludesLaterRecurrence(t *testing.T) {
	p := episodeProvider(t)
	rng := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(10 * time.Minute))}
	timeline, err := p.GetStatusTimeline(ctx, &v1.GetStatusTimelineRequest{Range: rng, Bucket: durationpb.New(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.GetFindingReadEpoch() != 3 {
		t.Errorf("Timeline boundary = %d, want captured epoch3", timeline.GetFindingReadEpoch())
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
	got, err := p.ListFindingEpisodes(ctx, &v1.ListFindingEpisodesRequest{Range: rng, ReadEpoch: proto.Int64(timeline.GetFindingReadEpoch())})
	if err != nil || got.GetTotalCount() != 2 {
		t.Fatalf("later recurrence entered selected Timeline: %v %v", got, err)
	}
	for _, episode := range got.Episodes {
		if episode.FirstRetainedEpoch > 3 {
			t.Fatalf("future occurrence %v", episode)
		}
	}
}

func TestTimelineEmptyBoundaryExcludesLateChecksInSameCollection(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.epoch(1, t0, "S0")
	p := &storeprov.Provider{St: st, Clock: func() time.Time { return t0.Add(time.Minute) }}
	rng := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(2 * time.Minute))}
	timeline, err := p.GetStatusTimeline(ctx, &v1.GetStatusTimelineRequest{Range: rng, Bucket: durationpb.New(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.FindingReadEpoch != 0 {
		t.Fatal("empty history unexpectedly had a transition boundary")
	}
	if _, err := st.ApplyCheckRuns(ctx, 1, []store.CheckRun{{Code: "stream-001", EntityType: "stream", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "APP/orders", Severity: "warning", Detail: "checks published later"}}}}); err != nil {
		t.Fatal(err)
	}
	got, err := p.ListFindingEpisodes(ctx, &v1.ListFindingEpisodesRequest{Range: rng, ReadEpoch: proto.Int64(timeline.FindingReadEpoch)})
	if err != nil || got.GetTotalCount() != 0 || len(got.GetUnavailable()) == 0 {
		t.Fatalf("late check publication entered selected empty history: %v %v", got, err)
	}
}

func TestTimelineReadBoundaryUsesOneConnection(t *testing.T) {
	p := episodeProvider(t)
	p.St.DB().SetMaxOpenConns(1)
	limited, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	got, err := p.GetStatusTimeline(limited, &v1.GetStatusTimelineRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(5 * time.Minute))}, Bucket: durationpb.New(time.Minute)})
	if err != nil || got.GetFindingReadEpoch() != 3 || len(got.GetNats()) == 0 {
		t.Fatalf("Timeline escaped its transaction connection: %v %v", got, err)
	}
}

func TestFindingEpisodeLatestStateMatchesRetainedFinding(t *testing.T) {
	for _, tc := range []struct {
		name string
		flap bool
		want v1.FindingState
	}{
		{"changed", false, v1.FindingState_FINDING_STATE_CHANGED},
		{"flapping", true, v1.FindingState_FINDING_STATE_FLAPPING},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := episodeProvider(t)
			r := rows{t, p.St}
			end := int64(4)
			if tc.flap {
				end = 9
			}
			for e := int64(4); e <= end; e++ {
				r.epoch(e, t0.Add(time.Duration(e-1)*time.Minute), "S0")
				var findings []store.FindingRow
				if !tc.flap || e%2 == 1 {
					findings = []store.FindingRow{{EntityKey: "APP/orders", Severity: "warning", Detail: "later state"}}
				}
				if _, err := p.St.ApplyCheckRuns(ctx, e, []store.CheckRun{{Code: "stream-001", EntityType: "stream", Status: store.RunOK, Rows: findings}}); err != nil {
					t.Fatal(err)
				}
			}
			p.Clock = func() time.Time { return t0.Add(10 * time.Minute) }
			latest, err := p.GetFinding(ctx, &v1.GetFindingRequest{Id: "stream-001:APP/orders"})
			if err != nil || latest.GetFinding().GetState() != tc.want {
				t.Fatalf("fixture latest: %v %v", latest, err)
			}
			got, err := p.ListFindingEpisodes(ctx, &v1.ListFindingEpisodesRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(30 * time.Second))}})
			if err != nil || len(got.GetEpisodes()) != 1 || got.Episodes[0].LatestRetainedState != tc.want || got.Episodes[0].Finding.State != v1.FindingState_FINDING_STATE_FIRING {
				t.Fatalf("historical/latest flags conflated: %v %v", got, err)
			}
		})
	}
}
