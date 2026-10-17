package fake

import (
	"cmp"
	"slices"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// placement is a stream's squares: the leader (its first peer) first, the
// rest by server name. A replica on a server that does not answer is
// offline, the follower the stream names behind, the others in sync.
func (s scen) placement(sp streamSpec) []*v1.ReplicaPlacement {
	peers := s.sc.peers(sp)
	var out []*v1.ReplicaPlacement
	for _, name := range peers[1:] {
		st := v1.PlacementState_PLACEMENT_STATE_IN_SYNC
		if sv, _ := s.sc.server(name); !sv.answering() {
			st = v1.PlacementState_PLACEMENT_STATE_OFFLINE
		} else if name == sp.behind {
			st = v1.PlacementState_PLACEMENT_STATE_BEHIND
		}
		out = append(out, &v1.ReplicaPlacement{Server: name, State: st})
	}
	slices.SortFunc(out, func(a, b *v1.ReplicaPlacement) int { return cmp.Compare(a.GetServer(), b.GetServer()) })
	return append([]*v1.ReplicaPlacement{{Server: peers[0], State: v1.PlacementState_PLACEMENT_STATE_LEADER}}, out...)
}

// streamCounts counts a scenario's streams of one kind (plain, KV_ or
// OBJ_ by prefix): all, by replicas, and with a lagging replica.
func (s scen) streamCounts(prefix string) (total, replicated, single, lagging int) {
	for _, sp := range s.sc.streams {
		kind := ""
		for _, p := range []string{"KV_", "OBJ_"} {
			if strings.HasPrefix(sp.name, p) {
				kind = p
			}
		}
		if kind != prefix {
			continue
		}
		total++
		switch n := len(s.sc.peers(sp)); {
		case n > 1:
			replicated++
		case n == 1:
			single++
		}
		if slices.ContainsFunc(s.placement(sp), func(p *v1.ReplicaPlacement) bool {
			return p.GetState() == v1.PlacementState_PLACEMENT_STATE_BEHIND
		}) {
			lagging++
		}
	}
	return total, replicated, single, lagging
}
