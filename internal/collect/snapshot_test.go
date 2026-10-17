package collect

import (
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func viewFor(s *Snapshot, name, id string) *ServerView {
	return s.view(server.ServerInfo{Name: name, ID: id})
}

func TestMissingAndPartial(t *testing.T) {
	s := NewSnapshot(time.Now())
	viewFor(s, "nats-0", "A")
	viewFor(s, "nats-1", "B")
	s.markMissing([]Endpoint{Varz})
	s.Expected = []string{"nats-0", "nats-1", "nats-2"}
	if got := s.Missing(); !reflect.DeepEqual(got, []string{"nats-2"}) {
		t.Fatalf("missing = %v", got)
	}
	if !s.Partial() {
		t.Fatal("expected partial")
	}
}

func TestCompleteEpochIsNotPartial(t *testing.T) {
	s := NewSnapshot(time.Now())
	v := viewFor(s, "nats-0", "A")
	v.Status[Varz] = EndpointStatus{State: StateOK}
	s.Expected = []string{"nats-0"}
	if s.Partial() {
		t.Fatal("did not expect partial")
	}
}

func TestExpectedIncludesPeers(t *testing.T) {
	s := NewSnapshot(time.Now())
	v := viewFor(s, "nats-0", "A")
	v.Routez = &server.Routez{Routes: []*server.RouteInfo{{RemoteName: "nats-1"}}}
	v.Jsz = &server.JSInfo{Meta: &server.MetaClusterInfo{Leader: "nats-0", Replicas: []*server.PeerInfo{{Name: "nats-2", Offline: true}}}}
	got := expectedNames([]string{"nats-3"}, s)
	want := []string{"nats-0", "nats-1", "nats-2", "nats-3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected = %v, want %v", got, want)
	}
}

func TestTrackerForgets(t *testing.T) {
	tr := NewTracker(time.Hour)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	tr.Seed("nats-0", now.Add(-30*time.Minute))
	tr.Seed("nats-1", now.Add(-2*time.Hour))
	tr.Seed("", now)
	if got := tr.Known(now); !reflect.DeepEqual(got, []string{"nats-0"}) {
		t.Fatalf("known = %v", got)
	}
}
