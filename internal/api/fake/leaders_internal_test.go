package fake

import (
	"strings"
	"testing"
	"time"
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
