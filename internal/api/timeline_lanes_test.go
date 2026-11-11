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
		{Bucket: 7, Kind: "client.churn", N: 0},
	})
	want := []*v1.ClientLaneBucket{{Index: 0, Connects: 6, Closes: 5}, {Index: 3, Closes: 1, Drops: 2, Shutdowns: 4, AuthErrors: 1}}
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
	if RestartMatches(&v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "A/s"}, "nats-1") || !RestartMatches(nil, "nats-1") {
		t.Error("restarts belong to the system and their server only")
	}
}
