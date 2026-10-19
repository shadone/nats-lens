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
// leaders from what the servers report.
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
