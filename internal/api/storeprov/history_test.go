package storeprov_test

import (
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
)

// applyEpoch adds an epoch and stores a run of every check for it; rows
// gives each check's status and rows.
func applyEpoch(t *testing.T, st *store.Store, set *checks.Set, epoch int64, at time.Time, rows func(code string) (string, []store.FindingRow)) {
	t.Helper()
	addEpochAt(t, st, epoch, at)
	var runs []store.CheckRun
	for _, c := range set.Checks() {
		status, rs := rows(c.Code)
		if status == "" {
			status = store.RunOK
		}
		runs = append(runs, store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: status, Rows: rs})
	}
	if _, err := st.ApplyCheckRuns(ctx, epoch, runs); err != nil {
		t.Fatal(err)
	}
}

func serveAt(t *testing.T, st *store.Store, set *checks.Set, clock time.Time) natslensv1connect.LensServiceClient {
	t.Helper()
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return clock }})
	return c
}

// server-002 fires in epoch 2 (t0+30s), resolves in 3 and fires again in
// 5: the first episode stays in a past range's findings and timeline.
func TestRefiredFindingKeepsItsEarlierEpisode(t *testing.T) {
	st, set, _ := findingsStore(t)
	for e := int64(4); e <= 5; e++ {
		applyEpoch(t, st, set, e, t0.Add(time.Duration(e-1)*30*time.Second), func(code string) (string, []store.FindingRow) {
			if code == "server-002" && e == 5 {
				return "", []store.FindingRow{{EntityKey: "nats-0", Severity: "critical", Detail: "again"}}
			}
			return "", nil
		})
	}
	c := serveAt(t, st, set, t0.Add(150*time.Second))
	r := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(90 * time.Second))}
	l := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: r, CheckCode: "server-002",
		States: []v1.FindingState{v1.FindingState_FINDING_STATE_FIRING, v1.FindingState_FINDING_STATE_RESOLVED}})))
	if len(l.GetFindings()) != 1 || l.GetFindings()[0].GetState() != v1.FindingState_FINDING_STATE_RESOLVED ||
		!l.GetFindings()[0].GetSince().AsTime().Equal(t0.Add(30*time.Second)) || l.GetFindings()[0].GetDetail() != "nats-0 healthz unavailable" {
		t.Fatalf("server-002 in [t0, t0+90s) = %v", l.GetFindings())
	}
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r, Bucket: durationpb.New(30 * time.Second)})))
	var row *v1.TimelineRow
	for _, x := range tl.GetRows() {
		if x.GetKey() == "server-002" {
			row = x
		}
	}
	if row == nil || row.GetCells()[1] != v1.Status_STATUS_CRITICAL || row.GetCells()[2] != v1.Status_STATUS_OK {
		t.Fatalf("server-002 row = %v", row)
	}
}

// A past range's statuses come from the findings as they stood at its
// end: nats-0 was critical in epoch 2, though server-002 resolved since.
func TestPastRangeStatusesUseThePastFindings(t *testing.T) {
	st, set, _ := findingsStore(t)
	c := serveAt(t, st, set, t0.Add(10*time.Hour))
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(25 * time.Second)), End: timestamppb.New(t0.Add(50 * time.Second))}
	servers := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: r}))).GetServers()
	if servers[0].GetName() != "nats-0" || servers[0].GetStatus() != v1.Status_STATUS_CRITICAL {
		t.Fatalf("nats-0 in epoch 2 = %v", servers[0])
	}
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: r, MaxPoints: 10})))
	if o.GetTally().GetCritical() != 2 || o.GetWorstFinding().GetCheckCode() == "" {
		t.Fatalf("tally at epoch 2 = %v, worst %v", o.GetTally(), o.GetWorstFinding())
	}
}

// A burst of findings fills its own lane limit and leaves the NATS events.
func TestLaneKeepsNATSEventsInAFindingBurst(t *testing.T) {
	st, set, _ := findingsStore(t)
	applyEpoch(t, st, set, 4, t0.Add(90*time.Second), func(code string) (string, []store.FindingRow) {
		if code == "client-001" {
			var rows []store.FindingRow
			for i := range 1100 {
				rows = append(rows, store.FindingRow{EntityKey: fmt.Sprintf("c%04d", i), Severity: "warning", Detail: "slow"})
			}
			return "", rows
		}
		return "", nil
	})
	c := serveAt(t, st, set, t0.Add(2*time.Minute))
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{
		Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(2 * time.Minute))}, Bucket: durationpb.New(30 * time.Second)})))
	nats, found := 0, 0
	for _, l := range tl.GetNats() {
		if l.GetSource() == v1.EventSource_EVENT_SOURCE_FINDING {
			found++
		} else {
			nats++
		}
	}
	if nats != 3 || found != 1000 || !tl.GetFindingTruncated() || tl.GetNatsTruncated() {
		t.Fatalf("lane: %d NATS, %d finding items, truncated %v/%v", nats, found, tl.GetNatsTruncated(), tl.GetFindingTruncated())
	}
}

// After a sweep that removed the epoch stream-005 started firing in, a past
// range still lists it and the timeline still draws it.
func TestSweptFiringStaysInPastAndTimeline(t *testing.T) {
	st, set, _ := findingsStore(t)
	e3 := t0.Add(60 * time.Second)
	if _, err := st.Sweep(ctx, e3.Add(time.Minute), 90*time.Second); err != nil { // keeps epoch 3 only
		t.Fatal(err)
	}
	r := &v1.TimeRange{Start: timestamppb.New(e3.Add(-10 * time.Second)), End: timestamppb.New(e3.Add(20 * time.Second))}
	past := serveAt(t, st, set, e3.Add(time.Hour))
	l := must(past.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: r, CheckCode: "stream-005"})))
	if l.GetTotalCount() != 1 || !l.GetFindings()[0].GetSince().AsTime().Equal(t0) {
		t.Fatalf("stream-005 in the past range = %v", l.GetFindings())
	}
	tl := must(past.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r, Bucket: durationpb.New(10 * time.Second)})))
	for _, row := range tl.GetRows() {
		if row.GetCheckCode() == "stream-005" && row.GetCells()[1] == v1.Status_STATUS_INFO {
			return
		}
	}
	t.Fatalf("no stream-005 row with INFO at epoch 3: %v", tl.GetRows())
}

// A resolved system finding's summary still names its entity.
func TestResolvedSystemFindingSummary(t *testing.T) {
	st, set, _ := findingsStore(t)
	for e := int64(4); e <= 5; e++ {
		applyEpoch(t, st, set, e, t0.Add(time.Duration(e-1)*30*time.Second), func(code string) (string, []store.FindingRow) {
			if code == "system-003" && e == 4 {
				return "", []store.FindingRow{{EntityKey: "", Severity: "warning", Detail: "meta leader changed 5 times"}}
			}
			return "", nil
		})
	}
	c := serveAt(t, st, set, t0.Add(150*time.Second))
	evs := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Kinds: []string{"finding.resolved"},
		Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(150 * time.Second))}}))).GetEvents()
	for _, e := range evs {
		if e.GetTransition().GetFindingId() == "system-003:" {
			if want := "resolved: system. Last failing result: meta leader changed 5 times"; e.GetSummary() != want {
				t.Fatalf("summary %q, want %q", e.GetSummary(), want)
			}
			return
		}
	}
	t.Fatalf("no resolve of system-003 in %v", evs)
}
