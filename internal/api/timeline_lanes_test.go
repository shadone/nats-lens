package api

import (
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A churn summary counts as connects and closes; a disconnect counts by
// its reason; buckets without a count are left out.
func TestClientLane(t *testing.T) {
	got := ClientLane([]ClientEventCount{
		{Bucket: 3, Kind: "client.disconnect", Reason: "Slow Consumer (Write Deadline)", N: 2},
		{Bucket: 0, Kind: "client.churn", N: 5},
		{Bucket: 0, Kind: "client.connect", N: 1},
		{Bucket: 3, Kind: "client.disconnect", Reason: "Client Closed", N: 1},
		{Bucket: 3, Kind: "client.disconnect", Reason: "Server Shutdown", N: 4},
		{Bucket: 3, Kind: "client.auth_error", N: 1},
		{Bucket: 3, Kind: "client.disconnect", Reason: "", N: 3},
		{Bucket: 7, Kind: "client.churn", N: 0},
	})
	want := []*v1.ClientLaneBucket{{Index: 0, Connects: 6, Closes: 5}, {Index: 3, Closes: 1, Drops: 2, Shutdowns: 4, AuthErrors: 1, UnknownDisconnects: 3}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i].String() != want[i].String() {
			t.Errorf("bucket %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// The newest items stay, oldest first; equal times order by event ID.
func TestNewestLane(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	item := func(id string, s int) *v1.LaneEvent {
		return &v1.LaneEvent{Id: id, Time: ts(at.Add(time.Duration(s) * time.Second))}
	}
	got, cut := NewestLane(2, false, []*v1.LaneEvent{item("10", 1), item("9", 1)}, []*v1.LaneEvent{item("restart:N1", 0)})
	if !cut || len(got) != 2 || got[0].GetId() != "9" || got[1].GetId() != "10" {
		t.Fatalf("got %v cut %v", got, cut)
	}
	if _, cut := NewestLane(5, true, got); !cut {
		t.Error("a part cut before stays cut")
	}
}

func TestRestartLaneEvent(t *testing.T) {
	e := RestartLaneEvent(ServerRestart{Server: "nats-1", NewID: "N1", OldVersion: "2.11.8", NewVersion: "2.12.2"})
	if e.GetLabel() != "nats-1 restarted, version 2.11.8 to 2.12.2" || e.GetEventId() != "" || e.GetKind() != RestartKind {
		t.Errorf("restart = %v", e)
	}
	if SubjectLanesFor(&v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-1"}) || !SubjectLanesFor(nil) {
		t.Error("only the system's timeline has the subject lanes")
	}
}

// Gaps are clipped to the range and merged; an open one runs to its end.
func TestCaptureGapSpans(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 17, 9, m, 0, 0, time.UTC) }
	got := CaptureGapSpans([]ReplayGap{
		{Kind: GapCollector, Start: at(0), End: at(15)},
		{Kind: GapEvents, Start: at(12), End: at(20)},
		{Kind: GapKubernetes, Start: at(30), End: at(40)},
		{Kind: GapEvents, Start: at(50)},
	}, at(10), at(55))
	if len(got) != 2 || !got[0].GetStart().AsTime().Equal(at(10)) || !got[0].GetEnd().AsTime().Equal(at(20)) ||
		!got[1].GetStart().AsTime().Equal(at(50)) || !got[1].GetEnd().AsTime().Equal(at(55)) {
		t.Fatalf("spans = %v", got)
	}
	if GapsCover(got, at(10), at(55)) || !GapsCover(got, at(11), at(19)) {
		t.Error("cover")
	}
}

// Adjacent, nested, before-range, after-range and open gaps.
func TestCaptureGapSpansEdges(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 17, 9, m, 0, 0, time.UTC) }
	got := CaptureGapSpans([]ReplayGap{
		{Kind: GapEvents, Start: at(20), End: at(25)},
		{Kind: GapCollector, Start: at(15), End: at(20)},
		{Kind: GapCollector, Start: at(16), End: at(18)},
		{Kind: GapCollector, Start: at(0), End: at(5)},
		{Kind: GapEvents, Start: at(70)},
		{Kind: GapEvents, Start: at(40)},
		{Kind: GapEvents, Start: at(45), End: at(50)},
	}, at(10), at(60))
	if len(got) != 2 || !got[0].GetStart().AsTime().Equal(at(15)) || !got[0].GetEnd().AsTime().Equal(at(25)) ||
		!got[1].GetStart().AsTime().Equal(at(40)) || !got[1].GetEnd().AsTime().Equal(at(60)) {
		t.Fatalf("spans = %v", got)
	}
	if !GapsCover(CaptureGapSpans([]ReplayGap{{Kind: GapEvents, Start: at(0), End: at(30)}, {Kind: GapCollector, Start: at(30)}}, at(10), at(60)), at(10), at(60)) {
		t.Error("adjacent gaps over the whole range do not cover it")
	}
}

// The system connection's gaps go to system events, an account
// connection's to that account's advisories; a collector gap to both.
func TestTimelineCaptureGaps(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 17, 9, m, 0, 0, time.UTC) }
	ev := func(m int, kind, conn string) *v1.Event {
		return &v1.Event{Time: ts(at(m)), Kind: kind, Raw: `{"connection":"` + conn + `"}`}
	}
	system, advisory, stopped := TimelineCaptureGaps(
		[]ReplayGap{{Kind: GapCollector, Start: at(50), End: at(52)}, {Kind: GapKubernetes, Start: at(0), End: at(60)}},
		[]*v1.Event{ev(10, "collector.connection_closed", "sys"), ev(20, "collector.reconnected", "sys"),
			ev(30, "collector.not_connected", "APP"), ev(31, "collector.disconnected", "collector")}, at(0), at(60))
	if stopped || len(system) != 2 || !system[0].GetStart().AsTime().Equal(at(10)) || !system[1].GetStart().AsTime().Equal(at(50)) {
		t.Errorf("system = %v, stopped %v", system, stopped)
	}
	if len(advisory) != 2 || advisory[0].GetConnection() != "APP" || !advisory[0].GetRange().GetEnd().AsTime().Equal(at(60)) ||
		advisory[1].GetConnection() != "" {
		t.Errorf("advisory = %v", advisory)
	}
	if _, _, stopped := TimelineCaptureGaps([]ReplayGap{{Kind: GapCollector, Start: at(0), End: at(60)}}, nil, at(10), at(20)); !stopped {
		t.Error("a collector gap over the range stops capture")
	}
}
