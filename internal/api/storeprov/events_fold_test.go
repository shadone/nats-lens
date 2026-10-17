package storeprov_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
)

// toolingName is the provider's own connection name in these tests.
const toolingName = "natslens-test"

// serveTooling serves st with toolingName as nats-lens's own connection.
func serveTooling(t *testing.T, st *store.Store) natslensv1connect.LensServiceClient {
	t.Helper()
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true,
		Clock: func() time.Time { return now }, ToolingNames: []string{toolingName}})
	return c
}

// ev is an event at t0 plus at.
func ev(at time.Duration, source, kind string) events.Event {
	return events.Event{Time: t0.Add(at), ReceivedAt: t0.Add(at), Source: source, Kind: kind, Summary: kind, Raw: json.RawMessage(`{}`)}
}

func write(t *testing.T, st *store.Store, evs ...events.Event) {
	t.Helper()
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
}

// wide is a range from an hour before t0 to an hour after.
func wide() *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(time.Hour))}
}

// routineMix writes 448 routine events (churn, sampled acks, API audit,
// account connection counts, tooling connects by name and from loopback)
// and 5 notable ones.
func routineMix(t *testing.T, st *store.Store) {
	t.Helper()
	var evs []events.Event
	add := func(n int, f func(i int) events.Event) {
		for i := range n {
			evs = append(evs, f(i))
		}
	}
	add(200, func(i int) events.Event {
		e := ev(-time.Duration(i)*time.Second, events.SourceSys, "client.churn")
		e.ClientName, e.ServerName = "stock-poller", "nats-0"
		return e
	})
	add(100, func(i int) events.Event {
		e := ev(-time.Duration(i)*time.Second, events.SourceJetStream, "consumer.ack_sampled")
		e.Stream, e.Consumer = "order-jobs", "resize-worker"
		return e
	})
	add(40, func(i int) events.Event {
		return ev(-time.Duration(i)*time.Second, events.SourceJetStream, "api.audit")
	})
	add(40, func(i int) events.Event {
		return ev(-time.Duration(i)*time.Second, events.SourceSys, "account.connections")
	})
	add(34, func(i int) events.Event {
		e := ev(-time.Duration(i)*time.Second, events.SourceSys, []string{"client.connect", "client.disconnect"}[i%2])
		e.ClientName = toolingName
		return e
	})
	add(34, func(i int) events.Event {
		e := ev(-time.Duration(i)*time.Second, events.SourceSys, []string{"client.connect", "client.disconnect"}[i%2])
		e.ClientName, e.ClientHost = "NATS CLI Version 0.2.2", "127.0.0.1"
		return e
	})
	add(5, func(i int) events.Event {
		e := ev(time.Duration(i)*time.Second, events.SourceJetStream, "stream.created")
		e.Stream = fmt.Sprintf("s%d", i)
		return e
	})
	write(t, st, evs...)
}

func TestHideRoutine(t *testing.T) {
	st := emptyStore(t)
	routineMix(t, st)
	c := serveTooling(t, st)
	res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide(), HideRoutine: true, IncludeFacets: true})))
	if res.GetRoutineHidden() != 448 || len(res.GetEvents()) != 5 || res.GetTotalCount() != 5 || res.GetTotalUnfiltered() != 453 {
		t.Fatalf("hidden %d, %d rows, total %d, unfiltered %d", res.GetRoutineHidden(), len(res.GetEvents()), res.GetTotalCount(),
			res.GetTotalUnfiltered())
	}
	for _, e := range res.GetEvents() {
		if e.GetRoutine() || e.GetKind() != "stream.created" {
			t.Errorf("shown: %v", e)
		}
	}
	groups := map[string]int32{}
	var sum int32
	for _, g := range res.GetRoutineGroups() {
		groups[g.GetKind()+" "+g.GetClientName()] = g.GetCount()
		sum += g.GetCount()
		if g.GetLabel() == "" {
			t.Errorf("group without a label: %v", g)
		}
	}
	if sum != 448 || groups["client.churn stock-poller"] != 200 || groups["client.connect "+toolingName] != 17 ||
		groups["client.disconnect NATS CLI Version 0.2.2"] != 17 {
		t.Fatalf("groups %v", res.GetRoutineGroups())
	}
	if res.GetRoutineGroups()[0].GetKind() != "client.churn" {
		t.Fatalf("largest group first: %v", res.GetRoutineGroups())
	}
	// Without hide_routine the routine events are listed and marked.
	all := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide(), FoldRepeats: true, PageSize: 1000})))
	var routine int
	for _, e := range all.GetEvents() {
		if e.GetRoutine() {
			routine += int(e.GetRepeats())
		}
	}
	if all.GetTotalCount() != 453 || all.GetRoutineHidden() != 0 || len(all.GetRoutineGroups()) != 0 || routine != 448 {
		t.Fatalf("everything: total %d, hidden %d, %d groups, %d routine", all.GetTotalCount(), all.GetRoutineHidden(),
			len(all.GetRoutineGroups()), routine)
	}
}

// A loopback client is tooling only in an epoch whose stored loopback
// decision is on, as the client checks judge it.
func TestLoopbackRoutineFollowsTheEpoch(t *testing.T) {
	st := fixtureStore(t)
	if err := st.SetLoopbackTooling(ctx, 3, false); err != nil {
		t.Fatal(err)
	}
	cli := func(at time.Duration) events.Event {
		e := ev(at, events.SourceSys, "client.connect")
		e.ClientName, e.ClientHost = "cli", "127.0.0.1"
		return e
	}
	write(t, st, cli(-10*time.Minute), cli(35*time.Second), cli(70*time.Second))
	c := serveTooling(t, st)
	res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide(), HideRoutine: true,
		ClientName: "cli"})))
	if res.GetRoutineHidden() != 2 || len(res.GetEvents()) != 1 || !res.GetEvents()[0].GetTime().AsTime().Equal(t0.Add(70*time.Second)) {
		t.Fatalf("hidden %d, shown %v", res.GetRoutineHidden(), res.GetEvents())
	}
}

func scheduling(at time.Duration) events.Event {
	e := ev(at, "k8s", "k8s.pod.failed_scheduling")
	e.Reason = "FailedScheduling"
	return e
}

func TestFoldRepeats(t *testing.T) {
	st := emptyStore(t)
	var evs []events.Event
	// Five within 6 minutes, then the same 11 minutes after the last.
	for i := range 5 {
		evs = append(evs, scheduling(time.Duration(i)*90*time.Second))
	}
	evs = append(evs, scheduling(6*time.Minute+11*time.Minute))
	// A Kubernetes Event that occurred 4 times is one row with count 4.
	counted := ev(-30*time.Minute, "k8s", "k8s.pod.backoff")
	counted.Raw = json.RawMessage(`{"uid":"u-1","count":4}`)
	evs = append(evs, counted)
	// A different stream does not fold with the same kind.
	a, b := ev(time.Second, events.SourceJetStream, "stream.created"), ev(2*time.Second, events.SourceJetStream, "stream.created")
	a.Stream, b.Stream = "a", "b"
	evs = append(evs, a, b)
	write(t, st, evs...)
	c := serveTooling(t, st)

	res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide(), FoldRepeats: true})))
	if res.GetTotalCount() != 9 || len(res.GetEvents()) != 5 {
		t.Fatalf("total %d, rows %v", res.GetTotalCount(), res.GetEvents())
	}
	got := map[string][]*v1.Event{}
	for _, e := range res.GetEvents() {
		got[e.GetKind()] = append(got[e.GetKind()], e)
	}
	sched := got["k8s.pod.failed_scheduling"]
	if len(sched) != 2 {
		t.Fatalf("scheduling rows %v", sched)
	}
	late, fold := sched[0], sched[1]
	if late.GetRepeats() != 1 || !late.GetFirstTime().AsTime().Equal(late.GetTime().AsTime()) {
		t.Errorf("11 minutes later folds: %v", late)
	}
	if fold.GetRepeats() != 5 || !fold.GetTime().AsTime().Equal(t0.Add(6*time.Minute)) || !fold.GetFirstTime().AsTime().Equal(t0) {
		t.Errorf("fold of five: repeats %d, %v to %v", fold.GetRepeats(), fold.GetFirstTime().AsTime(), fold.GetTime().AsTime())
	}
	if b := got["k8s.pod.backoff"]; len(b) != 1 || b[0].GetRepeats() != 4 {
		t.Errorf("counted Kubernetes event: %v", b)
	}
	if len(got["stream.created"]) != 2 {
		t.Errorf("streams a and b folded: %v", got["stream.created"])
	}

	// Without fold_repeats every event is a row and the new fields are
	// unset: the response of a client that predates them.
	old := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide()})))
	if old.GetTotalCount() != 9 || len(old.GetEvents()) != 9 || old.GetRoutineHidden() != 0 || len(old.GetFacets()) != 0 ||
		len(old.GetRoutineGroups()) != 0 || len(old.GetHistogram()) != 0 || old.GetTotalUnfiltered() != 0 {
		t.Fatalf("old response: %v", old)
	}
	for _, e := range old.GetEvents() {
		if e.GetRepeats() != 0 || e.GetFirstTime() != nil || e.GetRoutine() {
			t.Fatalf("old event: %v", e)
		}
	}
}

// Folded pages continue below the newest event of the previous page's
// last row: every row comes once, and events that arrive between pages
// change neither the rows of later pages nor their order.
func TestFoldedPagesAreStable(t *testing.T) {
	st := emptyStore(t)
	var evs []events.Event
	// 30 streams, each with 3 events a minute apart; the streams 20
	// minutes apart, so that none fold together, plus single events with
	// equal times.
	for s := range 30 {
		for i := range 3 {
			e := ev(-time.Duration(s)*20*time.Minute+time.Duration(i)*time.Minute, events.SourceJetStream, "stream.updated")
			e.Stream = fmt.Sprintf("s%02d", s)
			evs = append(evs, e)
		}
	}
	for i := range 7 {
		e := ev(-5*time.Minute, events.SourceSys, "server.lameduck")
		e.ServerName = fmt.Sprintf("n%d", i)
		evs = append(evs, e)
	}
	write(t, st, evs...)
	c := serveTooling(t, st)
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-24 * time.Hour)), End: timestamppb.New(now)}
	whole := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: r, FoldRepeats: true, PageSize: 1000})))
	if len(whole.GetEvents()) != 37 {
		t.Fatalf("%d rows, want 37", len(whole.GetEvents()))
	}
	var want []string
	for _, e := range whole.GetEvents() {
		want = append(want, e.GetId())
	}
	var paged []string
	tok := ""
	for page := 0; ; page++ {
		res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: r, FoldRepeats: true, PageSize: 4, PageToken: tok})))
		for _, e := range res.GetEvents() {
			paged = append(paged, e.GetId())
		}
		if page == 0 {
			// New events arrive: one joins the newest row, one starts a
			// new one.
			late := ev(80*time.Second, events.SourceSys, "server.lameduck")
			late.ServerName = "n0"
			other := ev(85*time.Second, events.SourceJetStream, "stream.deleted")
			write(t, st, late, other)
		}
		if tok = res.GetNextPageToken(); tok == "" {
			break
		}
	}
	if fmt.Sprint(paged) != fmt.Sprint(want) {
		t.Fatalf("paged %v\nwant  %v", paged, want)
	}
}

// Each facet counts the events of every filter except its own dimension,
// and its counts equal the unfolded counts of the same filters.
func TestFacets(t *testing.T) {
	st := emptyStore(t)
	var evs []events.Event
	add := func(n int, source, kind, server string) {
		for i := range n {
			e := ev(time.Duration(i)*time.Second, source, kind)
			e.ServerName = server
			evs = append(evs, e)
		}
	}
	add(3, events.SourceSys, "server.lameduck", "nats-0")
	add(4, events.SourceSys, "server.lameduck", "nats-1")
	add(2, events.SourceJetStream, "stream.leader_elected", "nats-1")
	add(5, "k8s", "k8s.pod.killing", "nats-1")
	add(6, "k8s", "k8s.pod.killing", "")
	write(t, st, evs...)
	c := serveTooling(t, st)
	server := func(name string) *v1.EntityRef {
		return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: name}
	}
	nats := []v1.EventSource{v1.EventSource_EVENT_SOURCE_NATS}
	res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide(), Sources: nats, Entity: server("nats-1"),
		IncludeFacets: true, FoldRepeats: true})))
	got := map[string]int32{}
	for _, f := range res.GetFacets() {
		got[f.GetDimension()+":"+f.GetValue()] = f.GetCount()
	}
	want := map[string]int32{
		"source:EVENT_SOURCE_NATS": 6, "source:EVENT_SOURCE_KUBERNETES": 5,
		"server:nats-0": 3, "server:nats-1": 6,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) || res.GetTotalUnfiltered() != 20 || res.GetTotalCount() != 6 {
		t.Fatalf("facets %v, unfiltered %d, total %d", got, res.GetTotalUnfiltered(), res.GetTotalCount())
	}
	if f := res.GetFacets()[0]; f.GetDimension() != "source" || f.GetValue() != "EVENT_SOURCE_NATS" {
		t.Fatalf("first facet %v", f)
	}
	count := func(req *v1.ListEventsRequest) int32 {
		req.Range = wide()
		return must(c.ListEvents(ctx, connect.NewRequest(req))).GetTotalCount()
	}
	if n := count(&v1.ListEventsRequest{Sources: []v1.EventSource{v1.EventSource_EVENT_SOURCE_KUBERNETES}, Entity: server("nats-1")}); n != 5 {
		t.Errorf("unfolded Kubernetes on nats-1: %d", n)
	}
	if n := count(&v1.ListEventsRequest{Sources: nats, Entity: server("nats-0")}); n != 3 {
		t.Errorf("unfolded NATS on nats-0: %d", n)
	}
}

func TestHistogram(t *testing.T) {
	st := emptyStore(t)
	routineMix(t, st)
	warn := ev(10*time.Second, events.SourceSys, "server.lameduck")
	crit := ev(11*time.Second, events.SourceJetStream, "stream.deleted")
	slow := ev(12*time.Second, events.SourceSys, "client.disconnect")
	slow.Reason, slow.ClientName = "Slow Consumer (Write Deadline)", "label-printer"
	write(t, st, warn, crit, slow)
	c := serveTooling(t, st)
	// 12 buckets of 5 minutes: t0 falls in the last one.
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-55 * time.Minute)), End: timestamppb.New(t0.Add(5 * time.Minute))}
	res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: r, HistogramBuckets: 12, HideRoutine: true})))
	h := res.GetHistogram()
	if len(h) != 12 {
		t.Fatalf("%d buckets", len(h))
	}
	for i, b := range h {
		if !b.GetStart().AsTime().Equal(t0.Add(-55*time.Minute + time.Duration(i)*5*time.Minute)) {
			t.Fatalf("bucket %d starts %v", i, b.GetStart().AsTime())
		}
	}
	// t0-4m59s .. t0-1s fall in bucket 10, t0 .. t0+12s in bucket 11.
	var routine int32
	for _, b := range h {
		routine += b.GetRoutine()
	}
	last := h[11]
	if routine != 448 || last.GetWarning() != 3 || last.GetNotable() != 5 {
		t.Fatalf("routine %d, last bucket %v", routine, last)
	}
	for _, bad := range []int32{-1, 121} {
		_, err := c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{HistogramBuckets: bad}))
		if codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%d buckets: %v", bad, err)
		}
	}
}
