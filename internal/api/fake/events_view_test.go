package fake_test

import (
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

// worthReading is the request of the Events page's "Worth reading" view.
func worthReading(system string) *v1.ListEventsRequest {
	return &v1.ListEventsRequest{System: system, HideRoutine: true, FoldRepeats: true, IncludeFacets: true,
		HistogramBuckets: 48, PageSize: 1000}
}

// row finds the folded row of a kind and summary text.
func row(evs []*v1.Event, kind, server string) *v1.Event {
	for _, e := range evs {
		if e.GetKind() == kind && e.GetServer() == server {
			return e
		}
	}
	return nil
}

func TestProdEventMix(t *testing.T) {
	c := client(t)
	res := must(c.ListEvents(ctx, connect.NewRequest(worthReading(""))))
	if res.GetRoutineHidden() < 400 || len(res.GetEvents()) > 140 || len(res.GetRoutineGroups()) < 4 {
		t.Fatalf("hidden %d, %d rows, %d groups", res.GetRoutineHidden(), len(res.GetEvents()), len(res.GetRoutineGroups()))
	}
	if res.GetTotalUnfiltered() != res.GetTotalCount()+res.GetRoutineHidden() {
		t.Fatalf("unfiltered %d, shown %d, hidden %d", res.GetTotalUnfiltered(), res.GetTotalCount(), res.GetRoutineHidden())
	}
	var tooling bool
	for _, g := range res.GetRoutineGroups() {
		if g.GetClientName() == "NATS CLI Version 0.2.2" {
			tooling = true
		}
	}
	if !tooling {
		t.Errorf("no tooling group in %v", res.GetRoutineGroups())
	}
	fold := row(res.GetEvents(), "k8s.pod.failed_scheduling", "")
	if fold == nil || fold.GetRepeats() != 5 || !fold.GetFirstTime().AsTime().Equal(at(11, 12, 5)) || !fold.GetTime().AsTime().Equal(at(11, 18, 0)) {
		t.Fatalf("failed scheduling fold: %v", fold)
	}
	var warnings int
	for _, e := range res.GetEvents() {
		if e.GetRoutine() {
			t.Errorf("routine row shown: %v", e)
		}
		if e.GetSeverity() >= v1.Severity_SEVERITY_WARNING {
			warnings++
		}
	}
	if warnings < 3 {
		t.Errorf("%d warning rows", warnings)
	}
	checkHistogram(t, res)
	checkFacets(t, c, "", res)
	// The cause of consumer-010 is the stored consumer.deleted.
	w := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: "consumer-010:APP/order-archive/analytics-aggregator"}))).GetFinding()
	cause := must(c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: w.GetCause().GetId()}))).GetEvent()
	if cause.GetKind() != "consumer.deleted" {
		t.Fatalf("cause %s is %v", w.GetCause().GetId(), cause)
	}
}

func TestGlobalEventMix(t *testing.T) {
	c := demo(t)
	res := must(c.ListEvents(ctx, connect.NewRequest(worthReading("example-global"))))
	// The failed logins add a row for label-printer's retries and one for
	// each burst of timeouts from 203.0.113.12.
	if res.GetRoutineHidden() < 600 || len(res.GetRoutineGroups()) < 4 || len(res.GetEvents()) > 135 {
		t.Fatalf("hidden %d, %d rows, groups %v", res.GetRoutineHidden(), len(res.GetEvents()), res.GetRoutineGroups())
	}
	if f := row(res.GetEvents(), "k8s.pod.failed_scheduling", "nats-us-4"); f == nil || f.GetRepeats() != 5 {
		t.Fatalf("failed scheduling fold: %v", f)
	}
	// The nine slow consumer disconnects from 13:38 fold into one row.
	var slow []int32
	for _, e := range res.GetEvents() {
		if e.GetKind() == "client.disconnect" && e.GetClientName() == "label-printer" && strings.Contains(e.GetSummary(), "Slow Consumer") {
			slow = append(slow, e.GetRepeats())
		}
	}
	if fmt.Sprint(slow) != "[2 9 1 1 1]" {
		t.Fatalf("slow consumer rows %v", slow)
	}
	checkHistogram(t, res)
	checkFacets(t, c, "example-global", res)
}

// The histogram counts every event of the range, routine ones included.
func checkHistogram(t *testing.T, res *v1.ListEventsResponse) {
	t.Helper()
	var sum, routine int32
	for _, b := range res.GetHistogram() {
		sum += b.GetWarning() + b.GetNotable() + b.GetRoutine()
		routine += b.GetRoutine()
	}
	if len(res.GetHistogram()) != 48 || sum != res.GetTotalUnfiltered() || routine != res.GetRoutineHidden() {
		t.Errorf("%d buckets, %d events, %d routine", len(res.GetHistogram()), sum, routine)
	}
}

// Each facet count equals the unfolded count of the same filter.
func checkFacets(t *testing.T, c natslensv1connect.LensServiceClient, system string, res *v1.ListEventsResponse) {
	t.Helper()
	if len(res.GetFacets()) < 3 {
		t.Fatalf("facets %v", res.GetFacets())
	}
	for _, f := range res.GetFacets() {
		req := &v1.ListEventsRequest{System: system, HideRoutine: true}
		switch f.GetDimension() {
		case "source":
			req.Sources = []v1.EventSource{v1.EventSource(v1.EventSource_value[f.GetValue()])}
		case "server":
			req.Entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: f.GetValue()}
		default:
			t.Fatalf("dimension %q", f.GetDimension())
		}
		if n := must(c.ListEvents(ctx, connect.NewRequest(req))).GetTotalCount(); n != f.GetCount() {
			t.Errorf("facet %s %s: %d, unfolded %d", f.GetDimension(), f.GetValue(), f.GetCount(), n)
		}
	}
}

// Folded pages hold every row of one big page once, in its order.
func TestFakeFoldedPages(t *testing.T) {
	c := client(t)
	req := worthReading("")
	req.HideRoutine = false
	whole := must(c.ListEvents(ctx, connect.NewRequest(req))).GetEvents()
	var got []string
	tok := ""
	for {
		req.PageSize, req.PageToken = 7, tok
		p := must(c.ListEvents(ctx, connect.NewRequest(req)))
		for _, e := range p.GetEvents() {
			got = append(got, e.GetId())
		}
		if tok = p.GetNextPageToken(); tok == "" {
			break
		}
	}
	var want []string
	for _, e := range whole {
		want = append(want, e.GetId())
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paged %v\nwant %v", got, want)
	}
	// A request without the new flags gets none of the new fields.
	old := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{PageSize: 1000})))
	if old.GetRoutineHidden() != 0 || len(old.GetFacets()) != 0 || len(old.GetHistogram()) != 0 || len(old.GetRoutineGroups()) != 0 ||
		old.GetTotalUnfiltered() != 0 {
		t.Fatalf("old response carries new fields")
	}
	for _, e := range old.GetEvents() {
		if e.GetRoutine() || e.GetRepeats() != 0 || e.GetFirstTime() != nil {
			t.Fatalf("old event %v", e)
		}
	}
}
