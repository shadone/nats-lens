package fake_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func dataHistory(t *testing.T, system string, d time.Duration) *v1.GetDataHistoryResponse {
	t.Helper()
	return must(demo(t).GetDataHistory(ctx, connect.NewRequest(&v1.GetDataHistoryRequest{System: system,
		Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-d)), End: timestamppb.New(fake.FixedNow)}})))
}

func gapLines(res *v1.GetDataHistoryResponse) []string {
	var out []string
	for _, g := range res.GetGaps() {
		end := "open"
		if g.GetEnd() != nil {
			end = g.GetEnd().AsTime().Format("01-02 15:04:05")
		}
		out = append(out, fmt.Sprintf("%s %s-%s", g.GetKind(), g.GetStart().AsTime().Format("01-02 15:04:05"), end))
	}
	return out
}

// example-staging tells the story: 9 hours of history, nats-lens down for
// 3 minutes 5 hours ago, the sys connection lost for 50 seconds, and the
// collector's connection closed 3 hours ago.
func TestDemoDataHistoryStaging(t *testing.T) {
	res := dataHistory(t, fake.StagingSystem, 24*time.Hour)
	want := []string{
		"HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED 10-17 11:30:00-open",
		"HISTORY_GAP_KIND_NOT_CONNECTED 10-17 11:29:56-open",
		"HISTORY_GAP_KIND_NOT_RUNNING 10-17 09:33:26-10-17 09:36:26",
		"HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED 10-17 07:30:00-10-17 07:30:50",
		"HISTORY_GAP_KIND_BEFORE_HISTORY 10-16 14:30:00-10-17 05:30:00",
	}
	if got := gapLines(res); !slices.Equal(got, want) {
		t.Errorf("gaps\n%v\nwant\n%v", got, want)
	}
	if res.GetRetention().AsDuration() != 14*24*time.Hour || res.GetArchive().GetEnabled() || res.GetFindingsAtStart() != 1 {
		t.Errorf("retention %v, archive %v, findings at start %d", res.GetRetention(), res.GetArchive(), res.GetFindingsAtStart())
	}
	// 9 hours of history; 3 hours and 3 minutes of them without data.
	if c := res.GetCoverage(); c.GetExpectedEpochs() != 1080 || c.GetEpochs() != 720-5 ||
		c.GetWithoutData().AsDuration() != 3*time.Hour+4*time.Second+3*time.Minute {
		t.Errorf("coverage %v", c)
	}
	for _, u := range res.GetUses() {
		if u.GetId() == "accept_review" && (u.GetEnough() || u.GetEnoughAt() == nil) {
			t.Errorf("accept review %v with 9 hours", u)
		}
	}
}

// The 3 minutes have no collection: no series point, no epoch, and
// Replay tells them as a gap.
func TestDemoStagingDownHasNoData(t *testing.T) {
	c := demo(t)
	from, to := time.Date(2026, 10, 17, 9, 33, 26, 0, time.UTC), time.Date(2026, 10, 17, 9, 36, 26, 0, time.UTC)
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: fake.StagingSystem, Metric: "cluster.connections",
		Range: &v1.TimeRange{Start: timestamppb.New(from.Add(-5 * time.Minute)), End: timestamppb.New(to.Add(5 * time.Minute))}, MaxPoints: 100}))).GetSeries()
	for _, p := range s.GetPoints() {
		if at := p.GetTime().AsTime(); at.After(from) && !at.Add(s.GetStep().AsDuration()).After(to) {
			t.Errorf("point at %v inside the time nats-lens was down", at)
		}
	}
	ov := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: fake.StagingSystem,
		Range: &v1.TimeRange{Start: timestamppb.New(from.Add(time.Minute)), End: timestamppb.New(to.Add(-time.Minute))}})))
	if ov.GetFreshness().GetEpoch() != 0 {
		t.Errorf("an epoch at %v inside the time nats-lens was down", ov.GetFreshness().GetEpochAt().AsTime())
	}
	res := replay(t, fake.StagingSystem, from.Add(-30*time.Minute), to.Add(30*time.Minute))
	found := false
	for _, st := range res.GetSteps() {
		found = found || st.GetGapKind() == v1.ReplayGapKind_REPLAY_GAP_KIND_COLLECTOR
	}
	if !found {
		t.Errorf("Replay over the time nats-lens was down has no gap step: %v", res.GetSteps())
	}
}

// example-prod has no gap in its last day and an archive of 90 days;
// example-global has a time without collections and one without the
// Kubernetes watch days back.
func TestDemoDataHistoryProdAndGlobal(t *testing.T) {
	prod := dataHistory(t, fake.Cluster, 24*time.Hour)
	if len(prod.GetGaps()) != 0 || prod.GetRetention().AsDuration() != 30*24*time.Hour {
		t.Errorf("prod gaps %v, retention %v", gapLines(prod), prod.GetRetention())
	}
	if a := prod.GetArchive(); !a.GetEnabled() || a.GetArchiveRetention().AsDuration() != 90*24*time.Hour || a.GetHours() != 90*24+1 {
		t.Errorf("prod archive %v", a)
	}
	global := gapLines(dataHistory(t, fake.GlobalSystem, 30*24*time.Hour))
	want := []string{
		"HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED 10-14 03:10:00-10-14 03:14:00",
		"HISTORY_GAP_KIND_NO_COLLECTIONS 10-13 02:10:26-10-13 02:15:26",
	}
	if !slices.Equal(global, want) {
		t.Errorf("global gaps\n%v\nwant\n%v", global, want)
	}
}
