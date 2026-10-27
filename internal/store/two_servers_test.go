package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

func twoServerView(id, name, tag, desc string) *collect.ServerView {
	return &collect.ServerView{
		Info:  server.ServerInfo{ID: id, Name: name},
		Connz: &server.Connz{ID: id, Total: 1, Conns: []*server.ConnInfo{{Cid: 7, Account: "APP", Name: "conn-" + tag}}},
		Subsz: &server.Subsz{ID: id, Total: 1, Subs: []server.SubDetail{{Account: "APP", Subject: "subj-" + tag, Cid: 7}}},
		Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "APP", Streams: []server.StreamDetail{{
			Name: "s", Created: t0, State: server.StreamState{Msgs: uint64(len(tag) * 100)},
			Config:  &server.StreamConfig{Name: "s", Storage: server.FileStorage, Description: desc},
			Cluster: &server.ClusterInfo{Leader: name, Replicas: []*server.PeerInfo{{Name: "peer-" + tag}}},
			Consumer: []*server.ConsumerInfo{{Stream: "s", Name: "c", Created: t0, NumPending: uint64(len(tag)),
				Config:  &server.ConsumerConfig{Durable: "c", Description: "cd-" + desc},
				Cluster: &server.ClusterInfo{Leader: name, Replicas: []*server.PeerInfo{{Name: "cpeer-" + tag}}}}},
		}}}}},
	}
}

// TestTwoServersBindTheirOwnIDs writes two servers per epoch and checks that
// every batch that binds the server ID puts each server's values beside
// its own ID, and that a configuration change on one server starts a run
// for it only.
func TestTwoServersBindTheirOwnIDs(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	for e, descB := range []string{"b1", "b2"} {
		snap := collect.NewSnapshot(t0)
		snap.Servers["SA'"] = twoServerView("SA'", "nats-a", "a", "a1")
		snap.Servers["SB"] = twoServerView("SB", "nats-b", "bb", descB)
		snap.FinishedAt = t0
		if _, err := s.WriteSnapshot(ctx, snap); err != nil {
			t.Fatalf("epoch %d: %v", e+1, err)
		}
	}
	db := s.DB()
	for _, c := range []struct {
		q    string
		want string
	}{
		{`SELECT string_agg(DISTINCT server_id || '=' || name, ',' ORDER BY server_id || '=' || name) FROM connections`, "SA'=conn-a,SB=conn-bb"},
		{`SELECT string_agg(DISTINCT server_id || '=' || subject, ',' ORDER BY server_id || '=' || subject) FROM subscriptions`, "SA'=subj-a,SB=subj-bb"},
		{`SELECT string_agg(DISTINCT server_id || '=' || messages, ',' ORDER BY server_id || '=' || messages) FROM stream_stats`, "SA'=100,SB=200"},
		{`SELECT string_agg(DISTINCT server_id || '=' || num_pending, ',' ORDER BY server_id || '=' || num_pending) FROM consumer_stats`, "SA'=1,SB=2"},
		{`SELECT string_agg(DISTINCT server_id || '=' || peer_name, ',' ORDER BY server_id || '=' || peer_name) FROM stream_replicas`, "SA'=peer-a,SB=peer-bb"},
		{`SELECT string_agg(DISTINCT server_id || '=' || peer_name, ',' ORDER BY server_id || '=' || peer_name) FROM consumer_replicas`, "SA'=cpeer-a,SB=cpeer-bb"},
		{`SELECT string_agg(server_id || '=' || description || ':' || first_epoch || '-' || last_epoch, ',' ORDER BY server_id, first_epoch) FROM stream_config`, "SA'=a1:1-2,SB=b1:1-1,SB=b2:2-2"},
		{`SELECT string_agg(server_id || '=' || description || ':' || first_epoch || '-' || last_epoch, ',' ORDER BY server_id, first_epoch) FROM consumer_config`, "SA'=cd-a1:1-2,SB=cd-b1:1-1,SB=cd-b2:2-2"},
	} {
		var got sql.NullString
		if err := db.QueryRow(c.q).Scan(&got); err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
		if got.String != c.want {
			t.Errorf("%s\n got %q\nwant %q", c.q, got.String, c.want)
		}
	}
}

// TestNullConnIsNotCountedAsReturned writes a CONNZ reply with a null
// entry: connz_returned counts only the connections stored, so a reader
// that takes connz_returned >= connz_total as "every connection listed"
// is not told the list is complete while a connection is missing.
func TestNullConnIsNotCountedAsReturned(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Varz, envelope(t, "nats-0", "N0", server.Varz{ID: "N0", Name: "nats-0"}))
	mustApply(t, snap, collect.Connz, envelope(t, "nats-0", "N0",
		json.RawMessage(`{"server_id":"N0","total":2,"connections":[null,{"cid":1,"account":"APP"}]}`)))
	snap.FinishedAt = t0
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	returned := count(t, s.DB(), `SELECT connz_returned FROM server_stats WHERE server_id = 'N0'`)
	rows := count(t, s.DB(), `SELECT count(*) FROM connections WHERE server_id = 'N0'`)
	if returned != rows {
		t.Errorf("connz_returned = %d, connections rows = %d", returned, rows)
	}
}

// TestNullConsumerIsNotCountedForTheAccount writes a JSZ reply with a null
// consumer entry: account_stats.js_consumers counts the consumers stored.
func TestNullConsumerIsNotCountedForTheAccount(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Varz, envelope(t, "nats-0", "N0", server.Varz{ID: "N0", Name: "nats-0"}))
	mustApply(t, snap, collect.Jsz, envelope(t, "nats-0", "N0", json.RawMessage(`{"server_id":"N0","account_details":[
  {"name":"APP","id":"APP","stream_detail":[{"name":"s","consumer_detail":[null,{"stream_name":"s","name":"c"}]}]}]}`)))
	snap.FinishedAt = t0
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	counted := count(t, s.DB(), `SELECT js_consumers FROM account_stats WHERE server_id = 'N0' AND account = 'APP'`)
	rows := count(t, s.DB(), `SELECT count(*) FROM consumer_stats WHERE server_id = 'N0'`)
	if counted != rows {
		t.Errorf("js_consumers = %d, consumer_stats rows = %d", counted, rows)
	}
}
