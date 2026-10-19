package fake

import (
	"cmp"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The demo's leaders follow its election events, as the store's follow
// the is_leader rows: an asset starts with its initial leader, changes
// lead only at an election, and has no leader while its leader does not
// answer until one is elected. Where the scenario writes no election, the
// model elects as NATS would: a second after the leader stops answering
// the first peer that answers (a consumer its stream's new leader when it
// answers), or the first peer to answer again when none did; it writes
// those elections as events too. Times are in the fixed frame.

// electionWait is how long after a leader stops answering a scenario's
// own election of the asset may come; without one by then the model
// elects.
const electionWait = 90 * time.Second

// leaderSeg is one leader of an asset from from on; "" for none.
type leaderSeg struct {
	from   time.Time
	server string
	// elected is whether an election began it, not the history's start.
	elected bool
}

// leaderModel is every asset's leaders over time, by entity key and
// "meta", and the elections the model made.
type leaderModel struct {
	segs      map[string][]leaderSeg
	elections []*v1.Event
}

// downSpan is a time a server did not answer: [from, to), to zero for
// one still not answering.
type downSpan struct{ from, to time.Time }

// downSpans are when a server did not answer, in the fixed frame: around
// its present process's start, to the second, and after it stopped for
// good.
func (sc *scenario) downSpans(sp serverSpec) []downSpan {
	s := scen{now: FixedNow, sc: sc}
	off := func(c time.Time) bool { return s.offline(sp.name, c) }
	var out []downSpan
	start := s.started(sp)
	var cur *downSpan
	for c := start.Add(-20 * time.Minute); c.Before(start.Add(5 * time.Minute)); c = c.Add(time.Second) {
		switch o := off(c); {
		case o && cur == nil:
			cur = &downSpan{from: c}
		case !o && cur != nil:
			cur.to = c
			out = append(out, *cur)
			cur = nil
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	if !sp.answering() && (len(out) == 0 || !out[len(out)-1].to.IsZero()) {
		out = append(out, downSpan{from: sp.down})
	}
	return out
}

// leaders builds the model once.
func (sc *scenario) leaders() *leaderModel {
	sc.modelOnce.Do(func() { sc.model = buildLeaders(sc) })
	return sc.model
}

// asset is one asset the model follows.
type asset struct {
	key, stream, consumer string
	peers                 []string
	initial               string
	// until is when a vanished durable went: it has no leader from then.
	until time.Time
}

func buildLeaders(sc *scenario) *leaderModel {
	s := scen{now: FixedNow, sc: sc}
	m := &leaderModel{segs: map[string][]leaderSeg{}}
	down := map[string][]downSpan{}
	for _, sp := range sc.servers {
		down[sp.name] = sc.downSpans(sp)
	}
	answers := func(name string, c time.Time) bool {
		for _, d := range down[name] {
			if !c.Before(d.from) && (d.to.IsZero() || c.Before(d.to)) {
				return false
			}
		}
		return true
	}
	// The scenario's own elections, by asset key, in time order.
	scripted := map[string][]*v1.Event{}
	for _, e := range sc.events(s, nil) {
		switch e.GetKind() {
		case "meta.leader_elected":
			scripted["meta"] = append(scripted["meta"], e)
		case "stream.leader_elected":
			scripted[streamRef(e.GetStream()).GetKey()] = append(scripted[streamRef(e.GetStream()).GetKey()], e)
		case "consumer.leader_elected":
			k := consumerRef(e.GetStream(), e.GetConsumer()).GetKey()
			scripted[k] = append(scripted[k], e)
		}
	}
	for _, evs := range scripted {
		slices.SortFunc(evs, func(a, b *v1.Event) int { return a.GetTime().AsTime().Compare(b.GetTime().AsTime()) })
	}
	var all []string
	for _, sp := range sc.servers {
		all = append(all, sp.name)
	}
	assets := []asset{{key: "meta", peers: all, initial: sc.metaLeader}}
	var consumers []asset
	for _, sp := range sc.streams {
		if sp.leaderless {
			continue
		}
		peers := sc.peers(sp)
		key := streamRef(sp.name).GetKey()
		init := cmp.Or(sc.initialLeaders[key], peers[0])
		assets = append(assets, asset{key: key, stream: sp.name, peers: peers, initial: init})
		gone := map[string]time.Time{}
		names := slices.Clone(sp.consumers)
		for _, v := range sp.vanished {
			names, gone[v.name] = append(names, v.name), v.at
		}
		for _, n := range names {
			ck := consumerRef(sp.name, n).GetKey()
			consumers = append(consumers, asset{key: ck, stream: sp.name, consumer: n, peers: peers, initial: cmp.Or(sc.initialLeaders[ck], init),
				until: gone[n]})
		}
	}
	// Streams and the meta group first: a consumer follows its stream's
	// new leader.
	for _, a := range append(assets, consumers...) {
		m.segs[a.key] = m.follow(a, scripted[a.key], down, answers)
	}
	slices.SortFunc(m.elections, func(a, b *v1.Event) int { return b.GetTime().AsTime().Compare(a.GetTime().AsTime()) })
	return m
}

// follow walks one asset's history: its scripted elections, and its
// leader stopping and its peers answering again.
func (m *leaderModel) follow(a asset, scripted []*v1.Event, down map[string][]downSpan, answers func(string, time.Time) bool) []leaderSeg {
	type point struct {
		at       time.Time
		election *v1.Event
		up       string // a peer answering again
		stop     string // a peer stopping
	}
	var pts []point
	for _, e := range scripted {
		pts = append(pts, point{at: e.GetTime().AsTime(), election: e})
	}
	for _, p := range a.peers {
		for _, d := range down[p] {
			pts = append(pts, point{at: d.from, stop: p})
			if !d.to.IsZero() {
				pts = append(pts, point{at: d.to, up: p})
			}
		}
	}
	slices.SortStableFunc(pts, func(x, y point) int { return x.at.Compare(y.at) })
	if !a.until.IsZero() {
		pts = slices.DeleteFunc(pts, func(p point) bool { return !p.at.Before(a.until) })
	}
	segs := []leaderSeg{{server: a.initial}}
	cur := a.initial
	set := func(at time.Time, server string, elected bool) {
		cur = server
		segs = append(segs, leaderSeg{from: at, server: server, elected: elected})
	}
	scriptedWithin := func(from time.Time) bool {
		return slices.ContainsFunc(scripted, func(e *v1.Event) bool {
			t := e.GetTime().AsTime()
			return t.After(from) && !t.After(from.Add(electionWait))
		})
	}
	elect := func(at time.Time) {
		candidates := a.peers
		if a.consumer != "" {
			if l := m.leaderOf(streamRef(a.stream).GetKey(), at); l != "" {
				candidates = append([]string{l}, a.peers...)
			}
		}
		for _, p := range candidates {
			if answers(p, at) {
				set(at, p, true)
				m.elections = append(m.elections, electionEvent(a, at, p))
				return
			}
		}
	}
	for _, pt := range pts {
		switch {
		case pt.election != nil:
			set(pt.at, pt.election.GetServer(), true)
		case pt.stop != "" && pt.stop == cur:
			set(pt.at, "", false)
			if !scriptedWithin(pt.at) {
				delay := time.Second
				if a.consumer != "" {
					delay = 2 * time.Second
				}
				elect(pt.at.Add(delay))
			}
		case pt.up != "" && cur == "" && !scriptedWithin(pt.at):
			delay := time.Second
			if a.consumer != "" {
				delay = 2 * time.Second
			}
			elect(pt.at.Add(delay))
		}
	}
	if !a.until.IsZero() {
		segs = append(segs, leaderSeg{from: a.until})
	}
	return segs
}

// leaderOf is the leader of key at the fixed-frame time c, as built so
// far.
func (m *leaderModel) leaderOf(key string, c time.Time) string {
	seg, _ := m.segAt(key, c)
	return seg.server
}

// segAt is the segment of key at c; ok is false for an asset the model
// does not follow.
func (m *leaderModel) segAt(key string, c time.Time) (leaderSeg, bool) {
	segs, ok := m.segs[key]
	if !ok {
		return leaderSeg{}, false
	}
	out := segs[0]
	for _, sg := range segs[1:] {
		if c.Before(sg.from) {
			break
		}
		out = sg
	}
	return out, true
}

// electionEvent is an election the model made, as the capture writes it.
func electionEvent(a asset, at time.Time, server string) *v1.Event {
	e := &v1.Event{Time: ts(at), Source: srcNATS, Server: server}
	switch {
	case a.key == "meta":
		e.Kind, e.Type, e.Summary = "meta.leader_elected", "LEADER_ELECTED", "meta leader -> "+server
	case a.consumer != "":
		e.Kind, e.Type, e.Account, e.Stream, e.Consumer = "consumer.leader_elected", "CONSUMER.LEADER_ELECTED", Account, a.stream, a.consumer
		e.Summary = a.consumer + " on " + a.stream + " -> " + server
	default:
		e.Kind, e.Type, e.Account, e.Stream = "stream.leader_elected", "STREAM.LEADER_ELECTED", Account, a.stream
		e.Summary = a.stream + " -> " + server
	}
	return e
}

// modelElections are the elections the model made, newest first, at the
// scenario's times.
func (s scen) modelElections() []*v1.Event {
	var out []*v1.Event
	for _, e := range s.sc.leaders().elections {
		c := proto.Clone(e).(*v1.Event)
		c.Time = ts(e.GetTime().AsTime().Add(s.shift))
		out = append(out, c)
	}
	return out
}

// leaderAt is the leader of the asset key at the scenario time at, def
// for an asset the model does not follow.
func (s scen) leaderAt(key, def string, at time.Time) string {
	seg, ok := s.sc.leaders().segAt(key, s.clock(at))
	if !ok {
		return def
	}
	return seg.server
}

// streamLeaderAt is a stream's leader at the scenario time at; "" for a
// leaderless stream or while none leads.
func (s scen) streamLeaderAt(sp streamSpec, at time.Time) string {
	if sp.leaderless {
		return ""
	}
	return s.leaderAt(streamRef(sp.name).GetKey(), s.leader(sp), at)
}

// consumerLeaderAt is a durable's leader at the scenario time at; its
// stream's for one the model does not follow.
func (s scen) consumerLeaderAt(sp streamSpec, name string, at time.Time) string {
	if sp.leaderless {
		return ""
	}
	return s.leaderAt(consumerRef(sp.name, name).GetKey(), s.streamLeaderAt(sp, at), at)
}

// metaLeaderAt is the meta leader at the scenario time at.
func (s scen) metaLeaderAt(at time.Time) string { return s.leaderAt("meta", s.sc.metaLeader, at) }

// leaderSince is when the leader of key at the scenario time at took the
// lead: the election that made it leader, or its process's start when it
// led from the history's start; never before that process started. Zero
// without a leader.
func (s scen) leaderSince(key string, at time.Time) time.Time {
	seg, ok := s.sc.leaders().segAt(key, s.clock(at))
	if !ok || seg.server == "" {
		return time.Time{}
	}
	var since time.Time
	if seg.elected {
		since = seg.from.Add(s.shift)
	}
	if sv, ok := s.sc.server(seg.server); ok {
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
