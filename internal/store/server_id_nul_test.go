package store

import (
	"context"
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// TestServerIDWithNUL writes two epochs of a server whose ID holds a NUL
// byte, quotes and a backslash through every JSON batch that stores the
// server ID: connections, subscriptions, stream and consumer stats,
// replicas and configuration versions. The ID is a parameter, so none of
// it reaches the SQL text.
func TestServerIDWithNUL(t *testing.T) {
	const id = "N\x00'y\"\\"
	s := openMem(t)
	ctx := context.Background()
	for range 2 {
		snap := collect.NewSnapshot(t0)
		snap.Servers[id] = &collect.ServerView{
			Info:  server.ServerInfo{ID: id, Name: "nats-0"},
			Connz: &server.Connz{ID: id, Conns: []*server.ConnInfo{{Cid: 1, Account: "APP"}}},
			Subsz: &server.Subsz{ID: id, Total: 1, Subs: []server.SubDetail{{Account: "APP", Subject: "a", Cid: 1}}},
			Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "APP", Streams: []server.StreamDetail{{
				Name: "s", Created: t0, Config: &server.StreamConfig{Name: "s"},
				Cluster: &server.ClusterInfo{Leader: "nats-0", Replicas: []*server.PeerInfo{{Name: "nats-1"}}},
				Consumer: []*server.ConsumerInfo{{Stream: "s", Name: "c", Created: t0, Config: &server.ConsumerConfig{Durable: "c"},
					Cluster: &server.ClusterInfo{Leader: "nats-0", Replicas: []*server.PeerInfo{{Name: "nats-1"}}}}},
			}}}}},
		}
		snap.FinishedAt = t0
		if _, err := s.WriteSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	for table, want := range map[string]int{
		"connections": 2, "subscriptions": 2, "stream_stats": 2, "consumer_stats": 2, "stream_replicas": 2,
		"consumer_replicas": 2, "stream_config": 1, "consumer_config": 1,
	} {
		if got := count(t, s.DB(), `SELECT count(*) FROM `+table+` WHERE server_id = ?`, id); got != want {
			t.Errorf("%s rows with the server ID = %d, want %d", table, got, want)
		}
	}
	if got := count(t, s.DB(), `SELECT count(*) FROM stream_config WHERE server_id = ? AND first_epoch = 1 AND last_epoch = 2`, id); got != 1 {
		t.Errorf("stream_config runs over epochs 1 to 2 = %d, want 1", got)
	}
}
