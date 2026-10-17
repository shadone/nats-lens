package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/events"
)

func TestK8sEventCounts(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	k8s := func(at time.Time, raw string) events.Event {
		return events.Event{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: "k8s.pod.killing", Summary: "x", Raw: json.RawMessage(raw)}
	}
	evs := []events.Event{
		k8s(t0, `{"uid":"a","count":1}`),
		k8s(t0.Add(time.Minute), `{"uid":"a","count":3}`),
		k8s(t0, `{"uid":"b","count":1}`),
		// A pod change carries the pod's UID but no count.
		k8s(t0, `{"uid":"pod-1"}`),
		k8s(t0.Add(-3*time.Hour), `{"uid":"old","count":1}`),
		sampleEvent(t0, "consumer.deleted"),
	}
	if err := s.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	got, err := s.K8sEventCounts(ctx, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["a"] != 3 || got["b"] != 1 {
		t.Fatalf("counts %v", got)
	}
}

func TestK8sLastState(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	_, _, last, err := s.K8sLastState(ctx, t0.Add(-time.Hour))
	if err != nil || !last.IsZero() {
		t.Fatalf("empty: %v %v", last, err)
	}
	k := func(m int, kind, raw string) events.Event {
		at := t0.Add(time.Duration(m) * time.Minute)
		return events.Event{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: kind, Summary: kind, Raw: json.RawMessage(raw)}
	}
	evs := []events.Event{
		k(0, "k8s.pod.not_ready", `{"namespace":"example","pod":"nats-0"}`),
		k(1, "k8s.pod.ready", `{"namespace":"example","pod":"nats-0"}`),
		k(2, "k8s.pod.restarted", `{"namespace":"example","pod":"nats-0"}`),
		k(0, "k8s.pod.not_ready", `{"namespace":"example","pod":"nats-1"}`),
		k(3, "k8s.pod.deleted", `{"namespace":"example","pod":"nats-2"}`),
		k(0, "k8s.node.cordoned", `{"node":"n0"}`),
		k(1, "k8s.node.ready", `{"node":"n0"}`),
		k(2, "k8s.node.deleted", `{"node":"n1"}`),
		// An Event-stream record names no pod in raw.pod.
		k(4, "k8s.pod.killing", `{"uid":"e","count":1}`),
	}
	if err := s.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	pods, nodes, last, err := s.K8sLastState(ctx, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !last.Equal(t0.Add(4 * time.Minute)) {
		t.Fatalf("last %v", last)
	}
	if p := pods["example/nats-0"]; p.Flag == nil || !*p.Flag || p.Deleted {
		t.Fatalf("nats-0 %+v", p)
	}
	if p := pods["example/nats-1"]; p.Flag == nil || *p.Flag {
		t.Fatalf("nats-1 %+v", p)
	}
	if p := pods["example/nats-2"]; p.Flag != nil || !p.Deleted {
		t.Fatalf("nats-2 %+v", p)
	}
	if n := nodes["n0"]; n.Flag == nil || !*n.Flag || n.Deleted {
		t.Fatalf("n0 %+v", n)
	}
	if n := nodes["n1"]; !n.Deleted || len(pods) != 3 || len(nodes) != 2 {
		t.Fatalf("n1 %+v, %d pods, %d nodes", n, len(pods), len(nodes))
	}
}

// A Kubernetes Event is one row per UID: a later count updates the row's
// count, message and receive time, and keeps its first time.
func TestK8sEventOneRowPerUID(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	ev := func(at time.Time, n int, note string) events.Event {
		return events.Event{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: "k8s.event", Reason: "FailedMount",
			Summary: "pod FailedMount: " + note, Raw: json.RawMessage(`{"uid":"u1","count":` + string(rune('0'+n)) + `,"note":"` + note + `"}`)}
	}
	for i := 1; i <= 6; i++ {
		if err := s.WriteEvents(ctx, []events.Event{ev(t0.Add(time.Duration(i)*time.Minute), i, "try "+string(rune('0'+i)))}); err != nil {
			t.Fatal(err)
		}
	}
	// Two increments in one batch as well.
	if err := s.WriteEvents(ctx, []events.Event{ev(t0.Add(7*time.Minute), 7, "try 7"), ev(t0.Add(8*time.Minute), 8, "try 8")}); err != nil {
		t.Fatal(err)
	}
	var n int
	var first, received time.Time
	var summary, count, firstSeen string
	if err := s.DB().QueryRow(`SELECT count(*), min(time), max(received_at), any_value(summary),
  any_value(json_extract_string(raw, '$.count')), any_value(json_extract_string(raw, '$.first_seen'))
FROM events WHERE source = 'k8s'`).Scan(&n, &first, &received, &summary, &count, &firstSeen); err != nil {
		t.Fatal(err)
	}
	if n != 1 || !first.Equal(t0.Add(time.Minute)) || !received.Equal(t0.Add(8*time.Minute)) || summary != "pod FailedMount: try 8" || count != "8" {
		t.Fatalf("rows %d first %v received %v summary %q count %s", n, first, received, summary, count)
	}
	if firstSeen == "" {
		t.Error("raw has no first_seen")
	}
	if got, err := s.K8sEventCounts(ctx, t0); err != nil || got["u1"] != 8 {
		t.Errorf("counts %v %v", got, err)
	}
}
