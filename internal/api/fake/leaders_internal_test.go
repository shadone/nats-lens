package fake

import (
	"slices"
	"strings"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Every election in the demo's events is won by a server that answered
// then, and agrees with the leader the scenario draws a second later.
func TestElectionsWonByAnsweringServers(t *testing.T) {
	for _, sys := range Demo(false) {
		p := sys.Provider.(*Provider)
		s := p.scen()
		for _, e := range p.events(s) {
			if !strings.HasSuffix(e.GetKind(), "leader_elected") {
				continue
			}
			at := e.GetTime().AsTime()
			if s.offline(e.GetServer(), at) {
				t.Errorf("%s: %s at %s won by %s, which did not answer then", sys.Ref.GetId(), e.GetKind(), at.Format("15:04:05"), e.GetServer())
			}
			after := at.Add(time.Second)
			var drawn string
			switch e.GetKind() {
			case "meta.leader_elected":
				drawn = s.metaLeaderAt(after)
			case "stream.leader_elected", "consumer.leader_elected":
				sp, err := s.sc.findStream(Account, e.GetStream())
				if err != nil {
					t.Fatal(err)
				}
				drawn = s.streamLeaderAt(sp, after)
				if e.GetConsumer() != "" {
					drawn = s.consumerLeaderAt(sp, e.GetConsumer(), after)
				}
			}
			if drawn != e.GetServer() {
				t.Errorf("%s: %s %s/%s at %s won by %s, drawn as led by %q", sys.Ref.GetId(), e.GetKind(), e.GetStream(), e.GetConsumer(),
					at.Format("15:04:05"), e.GetServer(), drawn)
			}
		}
	}
}

// assetKeys are the keys of every stream and durable the model follows,
// and "meta".
func assetKeys(s scen) []string {
	out := []string{"meta"}
	for k := range s.sc.leaders().segs {
		if k != "meta" {
			out = append(out, k)
		}
	}
	return out
}

// Every change of the leader drawn in one collection after another is an
// election event of that asset naming the new leader, as the store reads
// leaders from what the servers report; a single-replica asset, which
// has no election, leads again when its server answers.
func TestEveryLeaderChangeIsAnElection(t *testing.T) {
	for _, sys := range Demo(false) {
		p := sys.Provider.(*Provider)
		s := p.scen()
		elected := map[string][]*v1.Event{}
		for _, e := range p.events(s) {
			if !strings.HasSuffix(e.GetKind(), "leader_elected") {
				continue
			}
			k := "meta"
			if e.GetKind() == "stream.leader_elected" {
				k = streamRef(e.GetStream()).GetKey()
			} else if e.GetKind() == "consumer.leader_elected" {
				k = consumerRef(e.GetStream(), e.GetConsumer()).GetKey()
			}
			elected[k] = append(elected[k], e)
		}
		for _, k := range assetKeys(s) {
			if parts := strings.Split(k, "/"); len(parts) > 1 {
				if sp, err := s.sc.findStream(parts[0], parts[1]); err == nil && len(s.sc.peers(sp)) == 1 {
					continue
				}
			}
			prev := s.leaderAt(k, "", s.at(6, 0, 0))
			for at := s.at(6, 0, 30); at.Before(s.now); at = at.Add(sampleEvery) {
				cur := s.leaderAt(k, "", at)
				if cur != prev && cur != "" && !slices.ContainsFunc(elected[k], func(e *v1.Event) bool {
					t := e.GetTime().AsTime()
					return e.GetServer() == cur && t.After(at.Add(-sampleEvery)) && !t.After(at)
				}) {
					t.Errorf("%s: %s led by %q at %s, by %q before, without an election", sys.Ref.GetId(), k, cur, at.Format("15:04:05"), prev)
				}
				prev = cur
			}
		}
	}
}

// A leader's since is when it took the lead: it led from then on, and
// another server or none led the second before, unless it is the start
// of its process.
func TestLeaderSinceIsWhenTheLeadBegan(t *testing.T) {
	for _, sys := range Demo(false) {
		p := sys.Provider.(*Provider)
		s := p.scen()
		for _, at := range []time.Time{s.at(8, 50, 0), s.at(9, 5, 30), s.at(9, 16, 30), s.at(9, 20, 0), s.at(9, 30, 0), s.at(14, 0, 0)} {
			for _, k := range assetKeys(s) {
				l := s.leaderAt(k, "", at)
				if l == "" || k == "meta" {
					continue
				}
				since := s.leaderSince(k, at)
				sv, _ := s.sc.server(l)
				start := s.started(sv)
				if s.leaderAt(k, "", since) != l || (s.leaderAt(k, "", since.Add(-time.Second)) == l && !since.Equal(start) && start.Before(at)) {
					t.Errorf("%s: %s at %s led by %s since %s", sys.Ref.GetId(), k, at.Format("15:04:05"), l, since.Format("01-02 15:04:05"))
				}
			}
		}
	}
}

// A durable that went is elected nowhere after it went, and leads nothing.
func TestNoElectionOfAGoneDurable(t *testing.T) {
	for _, sys := range Demo(false) {
		p := sys.Provider.(*Provider)
		s := p.scen()
		for _, sp := range s.sc.streams {
			for _, v := range sp.vanished {
				at := v.at.Add(s.shift)
				for _, e := range p.events(s) {
					if e.GetKind() == "consumer.leader_elected" && e.GetStream() == sp.name && e.GetConsumer() == v.name && !e.GetTime().AsTime().Before(at) {
						t.Errorf("%s: %s elected at %s, after it went", sys.Ref.GetId(), v.name, e.GetTime().AsTime().Format("15:04:05"))
					}
				}
				if l := s.consumerLeaderAt(sp, v.name, at.Add(time.Minute)); l != "" {
					t.Errorf("%s: %s led by %s after it went", sys.Ref.GetId(), v.name, l)
				}
			}
		}
	}
}

// A durable whose leader stops a second before it goes is elected nowhere
// after it went, and its history stays in time order.
func TestFollowElectsNoGoneDurable(t *testing.T) {
	stop := fixed(9, 0, 0)
	down := map[string][]downSpan{"a": {{from: stop}}}
	answers := func(name string, c time.Time) bool { return name != "a" || c.Before(stop) }
	m := &leaderModel{segs: map[string][]leaderSeg{}}
	segs := m.follow(asset{key: "APP/s/x", stream: "s", consumer: "x", peers: []string{"a", "b"}, initial: "a", until: stop.Add(time.Second)},
		nil, down, answers)
	if len(m.elections) != 0 || !slices.IsSortedFunc(segs[1:], func(x, y leaderSeg) int { return x.from.Compare(y.from) }) ||
		segs[len(segs)-1].server != "" {
		t.Errorf("elections %v, segments %+v", m.elections, segs)
	}
}

// A single-replica asset has no raft group, so no election: its lead goes
// with its server and comes back with it.
func TestFollowElectsNoSingleReplica(t *testing.T) {
	stop, back := fixed(9, 0, 0), fixed(9, 2, 0)
	down := map[string][]downSpan{"a": {{from: stop, to: back}}}
	answers := func(name string, c time.Time) bool { return c.Before(stop) || !c.Before(back) }
	m := &leaderModel{segs: map[string][]leaderSeg{}}
	segs := m.follow(asset{key: "APP/s", stream: "s", peers: []string{"a"}, initial: "a"}, nil, down, answers)
	m.segs["APP/s"] = segs
	if len(m.elections) != 0 || m.leaderOf("APP/s", stop.Add(time.Minute)) != "" || m.leaderOf("APP/s", back.Add(time.Second)) != "a" {
		t.Errorf("elections %v, segments %+v", m.elections, segs)
	}
}
