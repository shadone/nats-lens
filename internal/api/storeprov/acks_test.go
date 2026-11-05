package storeprov_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

func ackRequest(id string, quiet time.Duration) *connect.Request[v1.AcknowledgeFindingRequest] {
	return connect.NewRequest(&v1.AcknowledgeFindingRequest{FindingId: id, Note: "Upgrade in progress; watching nats-2",
		QuietFor: durationpb.New(quiet), AckedBy: "alice@example.com"})
}

// An acknowledgement made through the API shows on its finding, in the
// tally and as an event of the finding's entity; a resolved finding is
// refused.
func TestAcknowledgeThroughTheStore(t *testing.T) {
	st, set, _ := findingsStore(t)
	clock := now
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return clock }})
	a := must(c.AcknowledgeFinding(ctx, ackRequest(missingID, 30*time.Minute))).GetAcknowledgement()
	if a.GetFindingId() != missingID || !a.GetQuiet() || !a.GetQuietUntil().AsTime().Equal(now.Add(30*time.Minute)) || a.GetEndedAt() != nil {
		t.Fatalf("acknowledgement = %v", a)
	}
	f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: missingID}))).GetFinding()
	if f.GetAcknowledgement().GetId() != a.GetId() || !f.GetAcknowledgement().GetQuiet() || f.GetMute() != nil {
		t.Errorf("finding's acknowledgement = %v", f.GetAcknowledgement())
	}
	tally := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))).GetTally()
	if tally.GetAcknowledged() != 1 || tally.GetWarning() != 1 {
		t.Errorf("tally = %v, want 1 warning, 1 acknowledged", tally)
	}
	// The event is at now; a range ends before its end.
	clock = now.Add(time.Second)
	evs := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Kinds: []string{store.AckEventKind}}))).GetEvents()
	if len(evs) != 1 || evs[0].GetSummary() != "job-cache-projector on order-jobs: acknowledged by alice@example.com: Upgrade in progress; watching nats-2" ||
		evs[0].GetConsumer() != "job-cache-projector" || evs[0].GetFinding().GetId() != missingID {
		t.Errorf("events = %v", evs)
	}
	// Later than its quiet, it is still on the finding, not quiet.
	clock = now.Add(31 * time.Minute)
	f = must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: missingID}))).GetFinding()
	if f.GetAcknowledgement() == nil || f.GetAcknowledgement().GetQuiet() {
		t.Errorf("after the quiet: %v", f.GetAcknowledgement())
	}
	if _, err := c.AcknowledgeFinding(ctx, ackRequest("server-002:nats-0", time.Hour)); codeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("resolved finding: %v, want failed_precondition", err)
	}
	if _, err := c.AcknowledgeFinding(ctx, ackRequest("server-002:nats-9", time.Hour)); codeOf(err) != connect.CodeNotFound {
		t.Errorf("unknown finding: %v, want not_found", err)
	}
	for id, want := range map[string]connect.Code{"007": connect.CodeInvalidArgument, "99": connect.CodeNotFound} {
		_, err := c.EndAcknowledgement(ctx, connect.NewRequest(&v1.EndAcknowledgementRequest{Id: id, EndedBy: "op"}))
		if codeOf(err) != want {
			t.Errorf("end %s: %v, want %v", id, err, want)
		}
	}
	end := must(c.EndAcknowledgement(ctx, connect.NewRequest(&v1.EndAcknowledgementRequest{Id: a.GetId(), EndedBy: "op"}))).GetAcknowledgement()
	if end.GetEndReason() != "released" || end.GetQuiet() {
		t.Errorf("ended = %v", end)
	}
	if f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: missingID}))).GetFinding(); f.GetAcknowledgement() != nil {
		t.Errorf("an ended acknowledgement shows: %v", f.GetAcknowledgement())
	}
}

// nats_lens_finding_acknowledged is 1 while the quiet holds and absent
// after; the count follows it.
func TestMetricsAcknowledged(t *testing.T) {
	st, set, _ := findingsStore(t)
	clock := now
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return clock }}
	_, c := apitest.Serve(t, p)
	must(c.AcknowledgeFinding(ctx, ackRequest(missingID, 30*time.Minute)))
	series := `nats_lens_finding_acknowledged{system="default",code="consumer-010",entity_type="consumer",entity_key="` + missingKey + `"} 1`
	body := writeMetrics(t, p)
	for _, want := range []string{series, `nats_lens_findings_acknowledged{system="default"} 1`} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing %q", want)
		}
	}
	clock = now.Add(31 * time.Minute)
	body = writeMetrics(t, p)
	if strings.Contains(body, "nats_lens_finding_acknowledged{") || !strings.Contains(body, `nats_lens_findings_acknowledged{system="default"} 0`+"\n") {
		t.Errorf("after the quiet:\n%s", body)
	}
}

// Only findings with the per-finding series get the acknowledged series;
// the count takes every entity.
func TestMetricsAcknowledgedFollowsTheCap(t *testing.T) {
	st, set, _ := findingsStore(t)
	addEpochAt(t, st, 4, t0.Add(90*time.Second))
	var runs []store.CheckRun
	for _, c := range set.Checks() {
		run := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK}
		if c.Code == "client-001" {
			for i := range storeprov.MaxFindingSeries + 7 {
				run.Rows = append(run.Rows, store.FindingRow{EntityKey: fmt.Sprintf("client-%03d", i), Severity: "warning", Detail: "slow"})
			}
		}
		runs = append(runs, run)
	}
	if _, err := st.ApplyCheckRuns(ctx, 4, runs); err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Checks: set, Clock: func() time.Time { return now }}
	_, c := apitest.Serve(t, p)
	for _, k := range []string{"client-000", "client-056"} {
		must(c.AcknowledgeFinding(ctx, ackRequest("client-001:"+k, time.Hour)))
	}
	body := writeMetrics(t, p)
	if !strings.Contains(body, `nats_lens_finding_acknowledged{system="default",code="client-001",entity_type="client",entity_key="client-000"} 1`+"\n") ||
		strings.Contains(body, `entity_key="client-056"`) || !strings.Contains(body, `nats_lens_findings_acknowledged{system="default"} 2`+"\n") {
		t.Errorf("metrics:\n%s", body)
	}
}

// A past range shows the acknowledgement in force at its end, quiet as it
// was then, whatever the quiet is now.
func TestAcknowledgementQuietAtThePastRangeEnd(t *testing.T) {
	st, set, _ := findingsStore(t)
	clock := now
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return clock }})
	must(c.AcknowledgeFinding(ctx, ackRequest(missingID, 30*time.Minute)))
	clock = now.Add(24 * time.Hour)
	yesterday := &v1.TimeRange{Start: timestamppb.New(now.Add(-time.Hour)), End: timestamppb.New(now.Add(10 * time.Minute))}
	var a *v1.Acknowledgement
	for _, f := range must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: yesterday}))).GetFindings() {
		if f.GetId() == missingID {
			a = f.GetAcknowledgement()
		}
	}
	if a == nil || !a.GetQuiet() {
		t.Errorf("acknowledgement at yesterday's range end = %v, want quiet", a)
	}
	f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: missingID}))).GetFinding()
	if f.GetAcknowledgement() == nil || f.GetAcknowledgement().GetQuiet() {
		t.Errorf("acknowledgement now = %v, want in force, not quiet", f.GetAcknowledgement())
	}
}
