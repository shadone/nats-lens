package storeprov

import (
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

func TestPlacementStates(t *testing.T) {
	cases := []struct {
		name string
		vs   map[string]*replicaView
		want []string
	}{
		{
			name: "leader, lagging, offline, other config, unlisted",
			vs: map[string]*replicaView{
				"n-e": {reports: true, hash: "a"},
				"n-d": {reports: true, hash: "b", listed: true, current: true},
				"n-c": {listed: true, offline: true},
				"n-b": {reports: true, hash: "a", listed: true},
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
			vs: map[string]*replicaView{
				"n-a": {reports: true, leader: true, hash: "new"},
				"n-b": {reports: true, hash: "old", listed: true},
			},
			want: []string{"n-a:PLACEMENT_STATE_LEADER", "n-b:PLACEMENT_STATE_BEHIND"},
		},
		{
			// Two servers claim the lead in one epoch: the first by name leads.
			name: "two leaders",
			vs: map[string]*replicaView{
				"n-c": {reports: true, leader: true, hash: "a", listed: true, current: true},
				"n-a": {reports: true, leader: true, hash: "a", listed: true, current: true},
				"n-b": {reports: true, hash: "a", listed: true, current: true},
			},
			want: []string{"n-a:PLACEMENT_STATE_LEADER", "n-b:PLACEMENT_STATE_IN_SYNC", "n-c:PLACEMENT_STATE_IN_SYNC"},
		},
		{
			// Without a leader nobody says who is current.
			name: "no leader",
			vs: map[string]*replicaView{
				"n-a": {reports: true, hash: "a"},
				"n-b": {reports: true, hash: "a"},
				"n-c": {reports: true, hash: "b"},
			},
			want: []string{"n-a:PLACEMENT_STATE_DISAGREES", "n-b:PLACEMENT_STATE_DISAGREES", "n-c:PLACEMENT_STATE_DISAGREES"},
		},
		{
			name: "no leader, one configuration",
			vs:   map[string]*replicaView{"n-a": {reports: true, hash: "a"}, "n-b": {reports: true, hash: "a"}},
			want: []string{"n-a:PLACEMENT_STATE_UNSPECIFIED", "n-b:PLACEMENT_STATE_UNSPECIFIED"},
		},
	}
	for _, c := range cases {
		// Map order varies between runs; the result must not.
		for range 20 {
			if got := states(placement(c.vs)); !slices.Equal(got, c.want) {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}
