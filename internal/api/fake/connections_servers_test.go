package fake_test

import (
	"slices"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListConnections names every server ListServers lists, sorted, whatever
// the server filter, the other filters and the page.
func TestListConnectionsNamesEveryServer(t *testing.T) {
	c := client(t)
	var want []string
	for _, s := range must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{}))).GetServers() {
		want = append(want, s.GetName())
	}
	slices.Sort(want)
	if len(want) < 2 {
		t.Fatalf("ListServers names = %v", want)
	}
	first := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{PageSize: 100})))
	reqs := map[string]*v1.ListConnectionsRequest{
		"no filter":     {},
		"server filter": {Server: want[0]},
		"no match":      {NameContains: "no-such-client"},
		"second page":   {PageSize: 100, PageToken: first.GetNextPageToken()},
	}
	for name, req := range reqs {
		got := must(c.ListConnections(ctx, connect.NewRequest(req)))
		if !slices.Equal(got.GetServers(), want) {
			t.Errorf("%s: servers = %v, want %v", name, got.GetServers(), want)
		}
	}
}
