package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The Accounts tile names the account with the most connections as the
// Accounts page counts them: clients summed over the servers plus the
// leaf nodes of the server that holds the most. APP has 30 clients;
// SUPPLIERS has 20 clients and 15 leaf nodes on nats-0 (5 on nats-1), so
// SUPPLIERS leads with 35, where a count of clients alone names APP.
func TestAccountsTileCountsLeafNodes(t *testing.T) {
	const keySup = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH"
	last := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	r := rows{t, st}
	r.epoch(1, last, "S0", "S1")
	for _, a := range []struct{ key, name string }{{keyApp, "APP"}, {keySup, "SUPPLIERS"}} {
		r.add("accounts", map[string]any{"account": a.key, "account_name": a.name, "is_system": false, "first_epoch": 1, "last_epoch": 1})
	}
	for i, id := range []string{"S0", "S1"} {
		r.add("servers", map[string]any{"server_id": id, "server_name": []string{"nats-0", "nats-1"}[i], "first_epoch": 1, "last_epoch": 1,
			"first_seen_at": last, "last_seen_at": last, "start_time": last.Add(-48 * time.Hour)})
	}
	for _, s := range []struct {
		server, account string
		conns, leafs    int
	}{{"S0", keyApp, 15, 0}, {"S1", keyApp, 15, 0}, {"S0", keySup, 10, 15}, {"S1", keySup, 10, 5}} {
		r.add("account_stats", map[string]any{"epoch": 1, "server_id": s.server, "account": s.account, "conns": s.conns,
			"leafnodes": s.leafs, "subscriptions": 1})
	}
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock }})
	rng := &v1.TimeRange{Start: timestamppb.New(last.Add(-time.Hour)), End: timestamppb.New(clock)}
	ov := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: rng})))
	var tile *v1.Tile
	for _, x := range ov.GetTiles() {
		if x.GetId() == api.TileAccounts {
			tile = x
		}
	}
	if want := "most connections: SUPPLIERS (35)"; tile.GetSubtitle() != want {
		t.Errorf("accounts subtitle %q, want %q", tile.GetSubtitle(), want)
	}
	// The Accounts page puts the same account first.
	page := must(c.GetAccountsAndAuth(ctx, connect.NewRequest(&v1.GetAccountsAndAuthRequest{Range: rng}))).GetAccounts()
	if len(page) == 0 || page[0].GetAccountName() != "SUPPLIERS" {
		t.Errorf("page's first account %v, want SUPPLIERS", page)
	}
}
