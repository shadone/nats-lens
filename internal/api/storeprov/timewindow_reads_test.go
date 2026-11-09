package storeprov

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
)

func TestFocusedTimeWindowsNativeParityAndReads(t *testing.T) {
	p, spy, r := pageScaleProvider(t, 1)
	at := r.End.AsTime().Add(-10 * time.Minute)
	if err := p.St.WriteEvents(context.Background(), []events.Event{
		{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: "k8s.node.drained", Raw: json.RawMessage(`{"node":"node-1","pool":"pool-1","servers":["nats-0"]}`)},
		{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: "k8s.pod.killing", ServerName: "nats-0"},
		{Time: at.Add(4 * time.Minute), ReceivedAt: at.Add(4 * time.Minute), Source: events.SourceK8s, Kind: "k8s.pod.ready", ServerName: "nats-0"},
	}); err != nil {
		t.Fatal(err)
	}
	spy.queries = 0
	full, err := p.GetOverview(context.Background(), &v1.GetOverviewRequest{Range: r})
	if err != nil {
		t.Fatal(err)
	}
	fullReads := spy.queries
	spy.queries = 0
	focused, err := p.GetTimeWindows(context.Background(), &v1.GetTimeWindowsRequest{Range: r})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Windows) == 0 {
		t.Fatal("positive window fixture missing")
	}
	if !proto.Equal(&v1.GetTimeWindowsResponse{Range: r, Windows: full.Windows}, focused) {
		t.Fatalf("focused evidence differs: %v / %v", full.Windows, focused)
	}
	if spy.queries != 2 || fullReads <= spy.queries {
		t.Fatalf("query counts: Overview=%d focused=%d", fullReads, spy.queries)
	}
	t.Logf("counted provider reads: Overview=%d focused=%d", fullReads, spy.queries)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spy.queries = 0
	got, err := p.GetTimeWindows(ctx, &v1.GetTimeWindowsRequest{Range: r})
	if got != nil || !errors.Is(err, context.Canceled) || spy.queries != 0 {
		t.Fatalf("cancellation: %v %v reads=%d", got, err, spy.queries)
	}
}
