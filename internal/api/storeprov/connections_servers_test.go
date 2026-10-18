package storeprov_test

import (
	"slices"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListConnections names every server ListServers lists, sorted and once
// each, whatever the server filter, the other filters and the page.
func TestListConnectionsNamesTheRangesServers(t *testing.T) {
	c := client(t)
	var want []string
	for _, s := range must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()}))).GetServers() {
		want = append(want, s.GetName())
	}
	// nats-1 has two processes in the fixture; its name comes once.
	if !slices.Equal(want, []string{"nats-0", "nats-1"}) {
		t.Fatalf("ListServers names = %v", want)
	}
	first := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), PageSize: 1})))
	reqs := map[string]*v1.ListConnectionsRequest{
		"no filter":     {Range: hour()},
		"server filter": {Range: hour(), Server: "nats-1"},
		"no match":      {Range: hour(), NameContains: "no-such-client"},
		"second page":   {Range: hour(), PageSize: 1, PageToken: first.GetNextPageToken()},
		"account":       {Range: hour(), Account: "NOPE"},
	}
	for name, req := range reqs {
		got := must(c.ListConnections(ctx, connect.NewRequest(req)))
		if !slices.Equal(got.GetServers(), want) {
			t.Errorf("%s: servers = %v, want %v", name, got.GetServers(), want)
		}
	}
	if got := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), Server: "nats-1"}))); got.GetTotalCount() == 0 ||
		slices.ContainsFunc(got.GetConnections(), func(x *v1.Connection) bool { return x.GetServer() != "nats-1" }) {
		t.Fatalf("server filter still filters the connections: %v", got.GetConnections())
	}
}
