package storeprov_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/shadone/nats-lens/internal/api"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func eventEvidenceProvider(t *testing.T) *storeprov.Provider {
	t.Helper()
	st := emptyStore(t)
	_, err := st.DB().Exec(`INSERT INTO events(id,time,received_at,source,kind,server_name,summary,raw) VALUES
 (9007199254740993,make_timestamp(?::BIGINT),make_timestamp(?::BIGINT),'k8s','k8s.node.drained','','Generic node drain','{"node":"node-a"}'),
 (9007199254740994,make_timestamp(?::BIGINT),make_timestamp(?::BIGINT),'k8s','k8s.pod.ready','nats-0','Generic pod ready','{"node":"node-a"}'),
 (9007199254740995,make_timestamp(?::BIGINT),make_timestamp(?::BIGINT),'sys','server.shutdown','nats-0','Generic shutdown','{}')`, t0.UnixMicro(), t0.UnixMicro(), t0.Add(time.Minute).UnixMicro(), t0.Add(time.Minute).UnixMicro(), t0.Add(2*time.Minute).UnixMicro(), t0.Add(2*time.Minute).UnixMicro())
	if err != nil {
		t.Fatal(err)
	}
	return &storeprov.Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return t0.Add(5 * time.Minute) }}
}
func eventEvidenceRange() *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(4 * time.Minute))}
}

func TestTimelineCapturesEventMembership(t *testing.T) {
	p := eventEvidenceProvider(t)
	ctx := context.Background()
	r := &v1.GetStatusTimelineRequest{Range: eventEvidenceRange(), Bucket: durationpb.New(time.Minute)}
	first, err := p.GetStatusTimeline(ctx, r)
	if err != nil || first.GetEventReadId() != 9007199254740995 {
		t.Fatalf("Timeline event membership absent: boundary=%d err=%v", first.GetEventReadId(), err)
	}
	if len(first.Windows) != 1 || len(first.Windows[0].SupportingEventIds) != 2 || first.Windows[0].GetEventReadId() != first.EventReadId {
		t.Fatalf("window lost exact capture: %v", first.Windows)
	}
	r.EventReadId = proto.Int64(9007199254740993)
	old, err := p.GetStatusTimeline(ctx, r)
	if err != nil || len(old.GetNats()) != 0 || len(old.GetKubernetes()) != 1 {
		t.Fatalf("later events entered capture: %v %v", old, err)
	}
	r.EventReadId = proto.Int64(0)
	empty, err := p.GetStatusTimeline(ctx, r)
	if err != nil || len(empty.GetNats()) != 0 || len(empty.GetKubernetes()) != 0 || len(empty.GetWindows()) != 0 || empty.GetEventReadId() != 0 {
		t.Fatalf("known empty event capture became live: %v %v", empty, err)
	}
}
func TestSelectedTimelineEventsStayExactAcrossFeedModes(t *testing.T) {
	p := eventEvidenceProvider(t)
	ctx := context.Background()
	for _, rich := range []bool{false, true} {
		t.Run(fmt.Sprint(rich), func(t *testing.T) {
			r := &v1.ListEventsRequest{Range: eventEvidenceRange(), PageSize: 50, SelectedIds: []string{"9007199254740994"}, IncludeFacets: rich, HistogramBuckets: 2}
			if !rich {
				r.HistogramBuckets = 0
			}
			got, err := p.ListEvents(ctx, r)
			if err != nil || got.GetTotalCount() != 1 || len(got.GetEvents()) != 1 || got.Events[0].Id != "9007199254740994" {
				t.Fatalf("selection broadened: %v %v", got, err)
			}
			if rich && got.TotalUnfiltered != 1 {
				t.Fatalf("outside-selection rows entered base total: %d", got.TotalUnfiltered)
			}
			r.EventReadId = proto.Int64(9007199254740993)
			before, err := p.ListEvents(ctx, r)
			if err != nil || before.GetTotalCount() != 0 || len(before.GetEvents()) != 0 {
				t.Fatalf("selection escaped event boundary: %v %v", before, err)
			}
		})
	}
	if _, err := p.GetEvent(ctx, &v1.GetEventRequest{Id: "9007199254740994", EventReadId: proto.Int64(9007199254740993)}); err == nil {
		t.Fatal("single-event read escaped capture")
	}
}

func TestSelectedEventResponsesAreByteBounded(t *testing.T) {
	p := eventEvidenceProvider(t)
	if _, err := p.St.DB().Exec(`UPDATE events SET summary=repeat('x',600000) WHERE id=9007199254740993`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ListEvents(context.Background(), &v1.ListEventsRequest{Range: eventEvidenceRange(), PageSize: 50, SelectedIds: []string{"9007199254740993"}}); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatalf("oversized selected page succeeded: %v", err)
	}
	if _, err := p.GetEvent(context.Background(), &v1.GetEventRequest{Id: "9007199254740993", EventReadId: proto.Int64(9007199254740995)}); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatalf("oversized selected event succeeded: %v", err)
	}
}
