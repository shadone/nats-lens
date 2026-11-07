package collect

import (
	"slices"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
)

// TestPeerNamesSkipNullEntries reads peers from ROUTEZ and JSZ replies
// with a null entry in the route list and in the meta group's replicas.
func TestPeerNamesSkipNullEntries(t *testing.T) {
	s := &Snapshot{Servers: map[string]*ServerView{"N0": {
		Routez: &server.Routez{Routes: []*server.RouteInfo{nil, {RemoteName: "nats-1"}}},
		Jsz: &server.JSInfo{Meta: &server.MetaClusterInfo{Leader: "nats-0",
			Replicas: []*server.PeerInfo{nil, {Name: "nats-2"}}}},
	}}}
	if got, want := peerNames(s), []string{"nats-1", "nats-0", "nats-2"}; !slices.Equal(got, want) {
		t.Errorf("peerNames = %q, want %q", got, want)
	}
}
