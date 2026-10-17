package fake

import (
	"fmt"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// newestFirst gives events IDs and times a minute apart, the last oldest.
func newestFirst(evs ...*v1.Event) []*v1.Event {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for i, e := range evs {
		e.Id, e.Time = fmt.Sprint(len(evs)-i), ts(t0.Add(-time.Duration(i)*time.Minute))
	}
	return evs
}

func TestFoldKeepsFindingsAndPodsApart(t *testing.T) {
	finding := func(id string) *v1.Event {
		return &v1.Event{Source: srcFinding, Kind: "finding.firing", Account: Account, Stream: "orders",
			Transition: &v1.FindingTransition{FindingId: id}}
	}
	pod := func(name string) *v1.Event {
		return &v1.Event{Source: srcK8s, Kind: "k8s.pod.failed_scheduling", Reason: "FailedScheduling",
			Summary: "pod shop/" + name + " FailedScheduling: 0/6 nodes are available"}
	}
	rows := fold(newestFirst(finding("stream-009:APP/orders"), finding("stream-001:APP/orders"), finding("stream-009:APP/orders")))
	if len(rows) != 2 {
		t.Fatalf("findings: %d rows", len(rows))
	}
	rows = fold(newestFirst(pod("w-0"), pod("w-1"), pod("w-2"), pod("w-3"), pod("w-4"), pod("stuck"), pod("stuck")))
	if len(rows) != 6 || rows[5].GetRepeats() != 2 {
		t.Fatalf("pods: %v", rows)
	}
}

func TestFakeAuditRoutine(t *testing.T) {
	audit := func(subject, reason string) *v1.Event {
		return &v1.Event{Kind: "api.audit", Reason: reason, Summary: subject + " by ops@10.0.0.1"}
	}
	cases := map[*v1.Event]bool{
		audit("$JS.API.STREAM.DELETE.orders", ""):              false,
		audit("$JS.API.STREAM.INFO.orders", "stream not found"): false,
		audit("$JS.API.STREAM.INFO.orders", ""):                true,
	}
	for e, want := range cases {
		if routineEvent(e) != want {
			t.Errorf("%s (%q): routine %v", e.GetSummary(), e.GetReason(), !want)
		}
	}
}

// A routine event is not coloured, as in the store.
func TestFakeRoutineHasNoSeverity(t *testing.T) {
	e := &v1.Event{Kind: "client.disconnect", Severity: v1.Severity_SEVERITY_WARNING, ClientHost: "127.0.0.1",
		Reason: "Slow Consumer (Write Deadline)"}
	markRoutine(e)
	if !e.GetRoutine() || e.GetSeverity() != v1.Severity_SEVERITY_UNSPECIFIED {
		t.Fatalf("routine %v, severity %v", e.GetRoutine(), e.GetSeverity())
	}
	n := &v1.Event{Kind: "server.lameduck", Severity: v1.Severity_SEVERITY_WARNING}
	markRoutine(n)
	if n.GetRoutine() || n.GetSeverity() != v1.Severity_SEVERITY_WARNING {
		t.Fatalf("notable event changed: %v", n)
	}
}
