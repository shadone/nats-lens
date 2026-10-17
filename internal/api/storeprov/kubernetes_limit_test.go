package storeprov

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// With more events than the limit, the newest window is the one shown.
func TestWindowsKeepTheNewestEvents(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	ev := func(m int, kind, server string) events.Event {
		at := base.Add(time.Duration(m) * time.Minute)
		return events.Event{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: kind, ServerName: server, Summary: kind,
			Raw: json.RawMessage(`{"node":"n0"}`)}
	}
	evs := []events.Event{
		ev(0, "k8s.node.drained", ""), ev(1, "k8s.pod.killing", "nats-0"), ev(2, "k8s.pod.ready", "nats-0"),
		ev(60, "k8s.node.drained", ""), ev(61, "k8s.pod.killing", "nats-1"),
	}
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	old := windowEventLimit
	windowEventLimit = 2
	defer func() { windowEventLimit = old }()
	p := &Provider{St: st}
	ws, err := p.k8sWindows(ctx, &v1.TimeRange{Start: timestamppb.New(base.Add(-time.Hour)), End: timestamppb.New(base.Add(2 * time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || !ws[0].GetStart().AsTime().Equal(base.Add(60*time.Minute)) {
		t.Fatalf("windows %v", ws)
	}
}
