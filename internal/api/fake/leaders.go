package fake

import (
	"slices"
	"time"
)

// leaderAt is the leader of the asset key at the scenario time at: the
// scenario's leader spell then, else def. A leader that does not answer
// then hands the lead to the first of peers that does, as an election
// would; "" when none does.
func (s scen) leaderAt(key, def string, peers []string, at time.Time) string {
	l := def
	for _, sp := range s.sc.leaders {
		if sp.key == key && (sp.from.IsZero() || !at.Before(sp.from.Add(s.shift))) && (sp.until.IsZero() || at.Before(sp.until.Add(s.shift))) {
			l = sp.server
		}
	}
	if l == "" || !s.offline(l, at) {
		return l
	}
	for _, p := range peers {
		if !s.offline(p, at) {
			return p
		}
	}
	return ""
}

// streamLeaderAt is a stream's leader at the scenario time at; "" for a
// leaderless stream.
func (s scen) streamLeaderAt(sp streamSpec, at time.Time) string {
	if sp.leaderless {
		return ""
	}
	return s.leaderAt(streamRef(sp.name).GetKey(), s.leader(sp), s.sc.peers(sp), at)
}

// consumerLeaderAt is a durable's leader at the scenario time at: its
// stream's unless the scenario says otherwise.
func (s scen) consumerLeaderAt(sp streamSpec, name string, at time.Time) string {
	if sp.leaderless {
		return ""
	}
	return s.leaderAt(consumerRef(sp.name, name).GetKey(), s.streamLeaderAt(sp, at), s.sc.peers(sp), at)
}

// metaLeaderAt is the meta leader at the scenario time at.
func (s scen) metaLeaderAt(at time.Time) string {
	var all []string
	for _, sv := range s.sc.servers {
		all = append(all, sv.name)
	}
	return s.leaderAt("meta", s.sc.metaLeader, all, at)
}

// leaderSince is when the leader at the scenario time at took the lead:
// the latest time before at at which another server led, by the spells
// of keys and the down times of peers, and never before the leader's
// process started.
func (s scen) leaderSince(keys, peers []string, leader string, at time.Time, leaderAt func(time.Time) string) time.Time {
	var edges []time.Time
	for _, sp := range s.sc.leaders {
		if slices.Contains(keys, sp.key) {
			for _, t := range []time.Time{sp.from, sp.until} {
				if !t.IsZero() {
					edges = append(edges, t.Add(s.shift))
				}
			}
		}
	}
	for _, p := range peers {
		edges = append(edges, s.downEdges(p)...)
	}
	slices.SortFunc(edges, func(a, b time.Time) int { return b.Compare(a) })
	var since time.Time
	for _, e := range edges {
		if e.After(at) {
			continue
		}
		if leaderAt(e.Add(-time.Nanosecond)) != leader {
			since = e
			break
		}
	}
	if sv, ok := s.sc.server(leader); ok {
		start := s.started(sv)
		if start.After(at) {
			start = s.on(2, 8, 12) // the process before, as server() lists it
		}
		if start.After(since) {
			since = start
		}
	}
	return since
}

// downEdges are the times a server stopped and began answering around
// its present process's start, and when it stopped for good.
func (s scen) downEdges(name string) []time.Time {
	sv, ok := s.sc.server(name)
	if !ok {
		return nil
	}
	var out []time.Time
	if !sv.answering() {
		out = append(out, sv.down.Add(s.shift))
	}
	start := s.started(sv)
	prev := s.offline(name, start.Add(5*time.Minute))
	for t := start.Add(5 * time.Minute); t.After(start.Add(-15 * time.Minute)); t = t.Add(-5 * time.Second) {
		if cur := s.offline(name, t); cur != prev {
			out = append(out, t.Add(5*time.Second))
			prev = cur
		}
	}
	return out
}
