package storeprov_test

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// TestKubernetesLaneAndWindows stores what the watcher records during a
// drain of nats-0's node and reads it back through the timeline, the
// overview and the event list.
func TestKubernetesLaneAndWindows(t *testing.T) {
	st := fixtureStore(t)
	k := func(at time.Duration, kind, typ, server, summary, raw string) events.Event {
		return events.Event{Time: t0.Add(at), ReceivedAt: t0.Add(at), Source: events.SourceK8s, Kind: kind, Type: typ,
			ServerName: server, Summary: summary, Raw: json.RawMessage(raw)}
	}
	evs := []events.Event{
		k(-10*time.Minute, "k8s.node.cordoned", "Cordoned", "", "node gke-pool-1-7f3a cordoned (unschedulable)",
			`{"node":"gke-pool-1-7f3a","pool":"pool-1","kubelet_version":"v1.33.4"}`),
		k(-9*time.Minute, "k8s.node.drained", "NodeDrain", "", "node gke-pool-1-7f3a cordoned and drained: nats-0 leaves it",
			`{"node":"gke-pool-1-7f3a","pool":"pool-1","kubelet_version":"v1.33.4","servers":["nats-0"]}`),
		k(-9*time.Minute, "k8s.pod.killing", "Killing", "nats-0", "pod nats-0 Killing: Stopping container nats", `{"uid":"e1","count":1}`),
		// A client pod's Killing is stored but not drawn.
		k(-9*time.Minute, "k8s.pod.killing", "Killing", "", "pod example/media-worker-1 Killing", `{"uid":"e2","count":1}`),
		k(-5*time.Minute, "k8s.pod.ready", "Ready", "nats-0", "pod nats-0 ready on gke-pool-1-91c2", `{"pod":"nats-0"}`),
		k(-4*time.Minute, "k8s.event", "SuccessfulAttachVolume", "nats-0", "pod nats-0 SuccessfulAttachVolume", `{"uid":"e3","count":1}`),
	}
	gap := events.Event{Time: t0.Add(-2 * time.Minute), ReceivedAt: t0, Source: events.SourceCollector, Kind: "collector.k8s_gap",
		Summary: "kubernetes nodes not watched for 1m", Raw: json.RawMessage(`{"seconds":60}`)}
	if err := st.WriteEvents(ctx, append(evs, gap)); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: hour(), Bucket: durationpb.New(15 * time.Minute)})))
	var kinds, shorts []string
	for _, e := range tl.GetKubernetes() {
		kinds, shorts = append(kinds, e.GetKind()), append(shorts, e.GetShortLabel())
		if e.GetEventId() == "" || e.GetId() != e.GetEventId() {
			t.Fatalf("lane item %v", e)
		}
	}
	want := []string{"k8s.node.cordoned", "k8s.node.drained", "k8s.pod.killing", "k8s.pod.ready", "collector.k8s_gap"}
	if len(kinds) != len(want) {
		t.Fatalf("kubernetes lane %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kubernetes lane %v, want %v", kinds, want)
		}
	}
	if shorts[2] != "nats-0 Killing" || shorts[1] != "node 7f3a" {
		t.Fatalf("short labels %v", shorts)
	}
	if tl.GetKubernetes()[0].GetSource() != v1.EventSource_EVENT_SOURCE_KUBERNETES ||
		tl.GetKubernetes()[4].GetSource() != v1.EventSource_EVENT_SOURCE_COLLECTOR ||
		tl.GetKubernetes()[1].GetSeverity() != v1.Severity_SEVERITY_WARNING {
		t.Fatalf("sources or severity %v", tl.GetKubernetes())
	}
	for _, e := range tl.GetNats() {
		if e.GetSource() == v1.EventSource_EVENT_SOURCE_KUBERNETES {
			t.Fatalf("kubernetes event on the NATS lane: %v", e)
		}
	}
	if len(tl.GetWindows()) != 1 {
		t.Fatalf("windows %v", tl.GetWindows())
	}
	w := tl.GetWindows()[0]
	if !w.GetStart().AsTime().Equal(t0.Add(-9*time.Minute)) || !w.GetEnd().AsTime().Equal(t0.Add(-5*time.Minute)) ||
		w.GetLabel() != "Node drain" || w.GetDetail() != "1 drain, 1 pod restart" {
		t.Fatalf("window %v", w)
	}

	// A server's timeline keeps the node events and only its own pods.
	other := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: hour(), Bucket: durationpb.New(15 * time.Minute),
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-1"}})))
	if n := len(other.GetKubernetes()); n != 3 {
		t.Fatalf("nats-1 lane %v", other.GetKubernetes())
	}

	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour()})))
	if len(o.GetWindows()) != 1 || o.GetWindows()[0].GetLabel() != "Node drain" {
		t.Fatalf("overview windows %v", o.GetWindows())
	}
	// A range that ends before the window has none.
	early := &v1.TimeRange{Start: hour().GetStart(), End: timestamppb.New(t0.Add(-20 * time.Minute))}
	if o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: early}))); len(o.GetWindows()) != 0 {
		t.Fatalf("early range windows %v", o.GetWindows())
	}

	l := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: hour(), PageSize: 50,
		Sources: []v1.EventSource{v1.EventSource_EVENT_SOURCE_KUBERNETES}})))
	if l.GetTotalCount() != int32(len(evs)) {
		t.Fatalf("kubernetes events %d, want %d", l.GetTotalCount(), len(evs))
	}
	for _, e := range l.GetEvents() {
		if e.GetSource() != v1.EventSource_EVENT_SOURCE_KUBERNETES {
			t.Fatalf("event %v", e)
		}
	}
}

// A crash loop, failed scheduling or OOM kill of a pod that is a NATS
// client (its name follows "@" in a connection name, or matches the
// configured pattern) is drawn on the Kubernetes lane as a warning; the
// same for any other pod is not.
func TestKubernetesLaneClientPodFailures(t *testing.T) {
	st := fixtureStore(t)
	if _, err := st.DB().Exec(`INSERT INTO connections (epoch, server_id, cid, name) VALUES
(3, 'NFAKESERVER0ID', 99, 'media-worker@media-worker-6b8c7d5f4e-q9w3z')`); err != nil {
		t.Fatal(err)
	}
	k := func(at time.Duration, kind, pod string) events.Event {
		return events.Event{Time: t0.Add(at), ReceivedAt: t0.Add(at), Source: events.SourceK8s, Kind: kind, Type: "Warning",
			Summary: "pod example/" + pod + " " + kind, Raw: json.RawMessage(`{"uid":"` + pod + kind + `","count":1,"object":{"kind":"Pod","name":"` + pod + `"}}`)}
	}
	evs := []events.Event{
		k(-3*time.Minute, "k8s.pod.backoff", "media-worker-6b8c7d5f4e-q9w3z"),
		k(-2*time.Minute, "k8s.pod.failed_scheduling", "unrelated-1"),
		k(-time.Minute, "k8s.pod.oom_killed", "batch-job-1"),
	}
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return now },
		ClientPods: regexp.MustCompile(`^batch-job-`)}
	_, c := apitest.Serve(t, p)
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: hour(), Bucket: durationpb.New(15 * time.Minute)})))
	var got []string
	for _, e := range tl.GetKubernetes() {
		got = append(got, e.GetKind())
		if e.GetSeverity() != v1.Severity_SEVERITY_WARNING {
			t.Errorf("%s severity %v, want warning", e.GetKind(), e.GetSeverity())
		}
	}
	if len(got) != 2 || got[0] != "k8s.pod.backoff" || got[1] != "k8s.pod.oom_killed" {
		t.Fatalf("lane %v, want the client pod's backoff and the pattern's OOM kill", got)
	}
}

// A stored upgrade on nodes with GKE's node pool label is a GKE node
// upgrade; one without the label is a node upgrade.
func TestKubernetesUpgradeWindowNamesThePlatform(t *testing.T) {
	st := fixtureStore(t)
	k := func(at time.Duration, kind, server, raw string) events.Event {
		return events.Event{Time: t0.Add(at), ReceivedAt: t0.Add(at), Source: events.SourceK8s, Kind: kind,
			ServerName: server, Summary: kind, Raw: json.RawMessage(raw)}
	}
	evs := []events.Event{
		k(-50*time.Minute, "k8s.node.upgrade", "", `{"node":"node-a","kubelet_version":"v1.33.5"}`),
		k(-49*time.Minute, "k8s.pod.killing", "nats-0", `{"uid":"e1","count":1}`),
		k(-20*time.Minute, "k8s.node.upgrade", "", `{"node":"gke-pool-1-7f3a","pool":"pool-1","kubelet_version":"v1.33.5"}`),
		k(-19*time.Minute, "k8s.pod.killing", "nats-1", `{"uid":"e2","count":1}`),
	}
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: hour(), Bucket: durationpb.New(15 * time.Minute)})))
	ws := tl.GetWindows()
	if len(ws) != 2 || ws[0].GetLabel() != "Node upgrade" || ws[1].GetLabel() != "GKE node upgrade" {
		t.Fatalf("windows %v", ws)
	}
}
