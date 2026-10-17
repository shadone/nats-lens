package storeprov

import (
	"database/sql"
	"slices"
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func states(ps []*v1.ReplicaPlacement) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.GetServer()+":"+p.GetState().String())
	}
	return out
}

var (
	yes  = sql.NullBool{Valid: true, Bool: true}
	no   = sql.NullBool{Valid: true}
	null = sql.NullBool{}
)

func TestPlacementStates(t *testing.T) {
	cases := []struct {
		name     string
		vs       map[string]*replicaReport
		replicas int32
		want     []string
	}{
		{
			name: "leader, lagging, offline, other config, unlisted",
			vs: map[string]*replicaReport{
				"n-e": {reports: true, hash: "a"},
				"n-d": {reports: true, hash: "b", listed: true, current: yes},
				"n-c": {listed: true, offline: yes},
				"n-b": {reports: true, hash: "a", listed: true, current: no},
				"n-a": {reports: true, leader: true, hash: "a"},
			},
			want: []string{
				"n-a:PLACEMENT_STATE_LEADER", "n-b:PLACEMENT_STATE_BEHIND", "n-c:PLACEMENT_STATE_OFFLINE",
				"n-d:PLACEMENT_STATE_DISAGREES", "n-e:PLACEMENT_STATE_DISAGREES",
			},
		},
		{
			// A follower catching up still reports the old configuration:
			// it is behind, not in disagreement.
			name: "catching up with the old configuration",
			vs: map[string]*replicaReport{
				"n-a": {reports: true, leader: true, hash: "new"},
				"n-b": {reports: true, hash: "old", listed: true, current: no, lag: sql.NullInt64{Valid: true, Int64: 40}},
			},
			want: []string{"n-a:PLACEMENT_STATE_LEADER", "n-b:PLACEMENT_STATE_BEHIND"},
		},
		{
			// Two servers claim the lead in one epoch: the first by name leads.
			name: "two leaders",
			vs: map[string]*replicaReport{
				"n-c": {reports: true, leader: true, hash: "a", listed: true, current: yes},
				"n-a": {reports: true, leader: true, hash: "a", listed: true, current: yes},
				"n-b": {reports: true, hash: "a", listed: true, current: yes},
			},
			want: []string{"n-a:PLACEMENT_STATE_LEADER", "n-b:PLACEMENT_STATE_IN_SYNC", "n-c:PLACEMENT_STATE_IN_SYNC"},
		},
		{
			// Without a leader nobody says who is current.
			name: "no leader",
			vs: map[string]*replicaReport{
				"n-a": {reports: true, hash: "a"},
				"n-b": {reports: true, hash: "a"},
				"n-c": {reports: true, hash: "b"},
			},
			want: []string{"n-a:PLACEMENT_STATE_DISAGREES", "n-b:PLACEMENT_STATE_DISAGREES", "n-c:PLACEMENT_STATE_DISAGREES"},
		},
		{
			name: "no leader, one configuration",
			vs:   map[string]*replicaReport{"n-a": {reports: true, hash: "a"}, "n-b": {reports: true, hash: "a"}},
			want: []string{"n-a:PLACEMENT_STATE_UNSPECIFIED", "n-b:PLACEMENT_STATE_UNSPECIFIED"},
		},
		{
			// current is unknown: a lag says behind, no lag says nothing.
			name: "current unknown",
			vs: map[string]*replicaReport{
				"n-a": {reports: true, leader: true, hash: "a"},
				"n-b": {reports: true, hash: "a", listed: true, current: null, lag: sql.NullInt64{Valid: true, Int64: 7}},
				"n-c": {reports: true, hash: "a", listed: true, current: null},
			},
			want: []string{"n-a:PLACEMENT_STATE_LEADER", "n-b:PLACEMENT_STATE_BEHIND", "n-c:PLACEMENT_STATE_UNSPECIFIED"},
		},
		{
			// A server that holds a copy the leader does not list disagrees
			// with the leader about the peer set.
			name: "reports but not listed",
			vs: map[string]*replicaReport{
				"n-a": {reports: true, leader: true, hash: "a"},
				"n-b": {reports: true, hash: "a"},
			},
			want: []string{"n-a:PLACEMENT_STATE_LEADER", "n-b:PLACEMENT_STATE_DISAGREES"},
		},
		{
			// The leader comes first, the rest by name, the padding last.
			name: "leader first, padded",
			vs: map[string]*replicaReport{
				"n-c": {reports: true, leader: true, hash: "a"},
				"n-a": {reports: true, hash: "a", listed: true, current: yes},
			},
			replicas: 3,
			want:     []string{"n-c:PLACEMENT_STATE_LEADER", "n-a:PLACEMENT_STATE_IN_SYNC", ":PLACEMENT_STATE_MISSING"},
		},
	}
	for _, c := range cases {
		// Map order varies between runs; the result must not.
		for range 20 {
			if got := states(placementOf(c.vs, c.replicas)); !slices.Equal(got, c.want) {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

// lagging counts exactly the replicas the placement calls BEHIND.
func TestLaggingMatchesBehind(t *testing.T) {
	stale := map[string]*replicaReport{
		"n-a": {reports: true, leader: true, hash: "new"},
		"n-b": {reports: true, hash: "old", listed: true, current: no},
	}
	if !lagging(placementOf(stale, 0)) {
		t.Fatal("a follower behind with a stale hash is not lagging")
	}
}
