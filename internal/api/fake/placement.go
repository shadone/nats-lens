package fake

import (
	"cmp"
	"slices"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// placement is a stream's squares now; placementAt's at a scenario time.
func (s scen) placement(sp streamSpec) []*v1.ReplicaPlacement {
	return s.placementAt(sp, s.lastEpoch(), false)
}

// epochTime is when the range's newest epoch started; the newest epoch
// when the range holds none.
func (s scen) epochTime(r *v1.TimeRange) time.Time {
	if at, _, ok := s.epochBefore(r); ok {
		return at
	}
	return s.lastEpoch()
}

// offline reports whether a server did not answer at the scenario time
// at: after it stopped answering, or while the scenario's series draw it
// down.
func (s scen) offline(name string, at time.Time) bool {
	sv, ok := s.sc.server(name)
	if !ok {
		return false
	}
	return (!sv.answering() && !at.Before(sv.down.Add(s.shift))) || s.downAt(sv, at)
}

// placementAt is a stream's squares at the scenario time at, by the store
// provider's rule: the leader then first, the rest by server name. A replica on a server offline then is offline, the follower the
// stream names missing, behind or disagreeing is so, the others in sync.
// Without a leader every reporting replica's state is unknown, and one on
// an offline server is a missing square without a server. A consumer's
// replicas (consumer true) follow their stream's, but are never behind or
// disagreeing.
func (s scen) placementAt(sp streamSpec, at time.Time, consumer bool) []*v1.ReplicaPlacement {
	return s.placementLed(sp, at, consumer, s.streamLeaderAt(sp, at))
}

// placementLed is placementAt with the leader given, for a consumer led
// apart from its stream.
func (s scen) placementLed(sp streamSpec, at time.Time, consumer bool, leader string) []*v1.ReplicaPlacement {
	peers := s.sc.peers(sp)
	if sp.leaderless {
		var out, gone []*v1.ReplicaPlacement
		for _, name := range peers {
			if s.offline(name, at) {
				gone = append(gone, &v1.ReplicaPlacement{State: v1.PlacementState_PLACEMENT_STATE_MISSING})
				continue
			}
			out = append(out, &v1.ReplicaPlacement{Server: name, State: v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED})
		}
		slices.SortFunc(out, func(a, b *v1.ReplicaPlacement) int { return cmp.Compare(a.GetServer(), b.GetServer()) })
		return append(out, gone...)
	}
	var out []*v1.ReplicaPlacement
	for _, name := range peers {
		if name == leader {
			continue
		}
		st := v1.PlacementState_PLACEMENT_STATE_IN_SYNC
		switch {
		case s.offline(name, at):
			st = v1.PlacementState_PLACEMENT_STATE_OFFLINE
		case name == sp.missing:
			st = v1.PlacementState_PLACEMENT_STATE_MISSING
		case name == sp.behind && !consumer:
			st = v1.PlacementState_PLACEMENT_STATE_BEHIND
		case name == sp.disagrees && !consumer:
			st = v1.PlacementState_PLACEMENT_STATE_DISAGREES
		}
		out = append(out, &v1.ReplicaPlacement{Server: name, State: st})
	}
	slices.SortFunc(out, func(a, b *v1.ReplicaPlacement) int { return cmp.Compare(a.GetServer(), b.GetServer()) })
	if leader == "" {
		return out
	}
	return append([]*v1.ReplicaPlacement{{Server: leader, State: v1.PlacementState_PLACEMENT_STATE_LEADER}}, out...)
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
