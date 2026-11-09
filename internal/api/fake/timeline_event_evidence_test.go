package fake

import (
	"context"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestDemoTimelineEventCaptureAndWindowInputs(t *testing.T) {
	p := New(false)
	r := &v1.GetStatusTimelineRequest{Range: &v1.TimeRange{Start: ts(FixedNow.Add(-24 * time.Hour)), End: ts(FixedNow)}, Bucket: durationpb.New(15 * time.Minute)}
	got, err := p.GetStatusTimeline(context.Background(), r)
	if err != nil || got.GetEventReadId() == 0 || len(got.GetWindows()) == 0 || len(got.Windows[0].SupportingEventIds) == 0 {
		t.Fatalf("demo evidence mapping absent: %v %v", got, err)
	}
	for _, w := range got.Windows {
		if w.GetEventReadId() != got.GetEventReadId() || w.CaptureRange == nil {
			t.Fatalf("window lost frame: %v", w)
		}
	}
	r.EventReadId = proto.Int64(0)
	empty, err := p.GetStatusTimeline(context.Background(), r)
	if err != nil || len(empty.Nats) != 0 || len(empty.Kubernetes) != 0 || len(empty.Windows) != 0 {
		t.Fatalf("empty frame became live: %v %v", empty, err)
	}
}
func TestDemoRecordedEventReadSurvivesClockAdvance(t *testing.T) {
	p := New(true)
	now := FixedNow
	p.clock = func() time.Time { return now }
	events := p.events(p.scen())
	e := events[0]
	boundary := now.Unix()*storageFrameScale + num(e.Id)
	first, err := p.GetEvent(context.Background(), &v1.GetEventRequest{Id: e.Id, EventReadId: proto.Int64(boundary)})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	next, err := p.GetEvent(context.Background(), &v1.GetEventRequest{Id: e.Id, EventReadId: proto.Int64(boundary)})
	if err != nil || !proto.Equal(first.Event, next.GetEvent()) {
		t.Fatalf("same recorded identity moved with source clock: first=%v next=%v err=%v", first, next, err)
	}
	rangeAt := &v1.TimeRange{Start: ts(FixedNow.Add(-24 * time.Hour)), End: ts(FixedNow)}
	for _, rich := range []bool{false, true} {
		req := &v1.ListEventsRequest{Range: rangeAt, PageSize: 50, SelectedIds: []string{e.Id}, EventReadId: proto.Int64(boundary), IncludeFacets: rich}
		page, err := p.ListEvents(context.Background(), req)
		if err != nil || len(page.GetEvents()) != 1 || page.TotalCount != 1 || !proto.Equal(page.Events[0].Time, e.Time) {
			t.Fatalf("selected demo frame broadened/moved: %v %v", page, err)
		}
	}
}

func TestDemoWindowEvidenceKeepsOriginalPayload(t *testing.T) {
	p := New(false)
	events := p.events(p.scen())
	var chosen *v1.Event
	for _, e := range events {
		if e.Source == srcK8s && e.Raw != "" {
			chosen = e
			break
		}
	}
	if chosen == nil {
		t.Fatal("fixture has no original Kubernetes payload")
	}
	got, err := p.GetEvent(context.Background(), &v1.GetEventRequest{Id: chosen.Id})
	if err != nil || got.GetEvent().GetRaw() != chosen.Raw {
		t.Fatalf("derivation payload replaced by generic fields: %v", err)
	}
}

func TestDemoWindowEventBatchReplaysExactFrame(t *testing.T) {
	p := New(true)
	now := FixedNow
	p.clock = func() time.Time { return now }
	events := p.events(p.scen())
	e := events[0]
	boundary := now.Unix()*storageFrameScale + num(e.Id)
	now = now.Add(time.Hour)
	got, err := p.GetEventBatch(context.Background(), &v1.GetEventBatchRequest{Ids: []string{e.Id}, EventReadId: proto.Int64(boundary)})
	if err != nil || len(got.GetEvents()) != 1 || !proto.Equal(got.Events[0].Time, e.Time) {
		t.Fatalf("batch frame changed: count=%d err=%v", len(got.GetEvents()), err)
	}
}

func TestDemoEventBatchSummariesDeferPayloads(t *testing.T) {
	p := New(false)
	capture, err := p.captureEvents(nil)
	if err != nil {
		t.Fatal(err)
	}
	var chosen []*v1.Event
	var ids []string
	for _, e := range capture.events {
		if e.Source == srcK8s && e.Raw != "" {
			chosen = append(chosen, e)
			ids = append(ids, e.Id)
			if len(ids) == 2 {
				break
			}
		}
	}
	if len(ids) != 2 {
		t.Fatal("fixture requires two original payloads")
	}
	page, err := p.GetEventBatch(context.Background(), &v1.GetEventBatchRequest{Ids: ids, EventReadId: proto.Int64(capture.boundary)})
	if err != nil || len(page.GetEvents()) != 2 {
		t.Fatalf("batch unavailable: %v", err)
	}
	for _, summary := range page.Events {
		if summary.Raw != "" {
			t.Fatalf("summary batch eagerly returns payload for event %s", summary.Id)
		}
	}
	for _, original := range chosen {
		detail, err := p.GetEvent(context.Background(), &v1.GetEventRequest{Id: original.Id, EventReadId: proto.Int64(capture.boundary)})
		if err != nil || detail.GetEvent().GetRaw() != original.Raw || original.Raw == "" {
			t.Fatalf("explicit detail lost original payload: %v", err)
		}
	}
}

func TestDemoCapturedEventSelectionReopensItsFrame(t *testing.T) {
	p := New(true)
	now := FixedNow
	p.clock = func() time.Time { return now }
	capture, err := p.captureEvents(nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{capture.events[0].Id, capture.events[1].Id}
	r := &v1.ListEventSelectionRequest{Ids: ids, EventReadId: proto.Int64(capture.boundary), PageSize: 1}
	a, err := p.ListEventSelection(context.Background(), r)
	if err != nil || a.GetSelectedTotalCount() != 2 || a.GetNextOffset() != 1 || len(a.GetEvents()) != 1 {
		t.Fatalf("first selection missing: %v %v", a, err)
	}
	now = now.Add(time.Hour)
	b, err := p.ListEventSelection(context.Background(), r)
	if err != nil || !proto.Equal(a, b) {
		t.Fatalf("selection moved with clock: %v", err)
	}
	r.Offset = 1
	c, err := p.ListEventSelection(context.Background(), r)
	if err != nil || c.GetNextOffset() != 0 || len(c.GetEvents()) != 1 || c.Events[0].Id != ids[1] || c.Events[0].Raw != "" {
		t.Fatalf("last selection page changed: %v %v", c, err)
	}
}
