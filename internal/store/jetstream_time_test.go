package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// jszView is one server's JSZ with n R3 streams of perStream consumers
// each in one account, placed on peers (the server's own name among them)
// and led by peers[0]. Without configs the streams and consumers carry no
// configuration, so that no configuration version is written.
func jszView(id, name string, peers []string, n, perStream int, at time.Time, configs bool) *collect.ServerView {
	cluster := func(group string) *server.ClusterInfo {
		since := at.Add(-time.Hour)
		ci := &server.ClusterInfo{Name: "example-dev", RaftGroup: group, Leader: peers[0], LeaderSince: &since}
		for _, p := range peers {
			if p != name {
				ci.Replicas = append(ci.Replicas, &server.PeerInfo{Name: p, Current: true, Active: 120 * time.Millisecond,
					Lag: 3, Peer: "peer-" + p})
			}
		}
		return ci
	}
	acc := &server.AccountDetail{Name: "APP", Id: "APP"}
	for i := range n {
		created := at.Add(-time.Duration(i+1) * time.Minute)
		s := server.StreamDetail{
			Name: fmt.Sprintf("orders-%d", i), Created: created, Cluster: cluster(fmt.Sprintf("S-%d", i)),
			State: server.StreamState{Msgs: uint64(i) * 100, Bytes: uint64(i) << 20, FirstSeq: 1, LastSeq: uint64(i) * 100,
				FirstTime: created, LastTime: at, NumSubjects: i % 10, Consumers: perStream},
		}
		if configs {
			s.Config = &server.StreamConfig{Name: s.Name, Subjects: []string{fmt.Sprintf("orders.%d.>", i)},
				Retention: server.LimitsPolicy, Storage: server.FileStorage, Replicas: len(peers), MaxConsumers: -1,
				MaxMsgs: -1, MaxBytes: -1, MaxAge: 24 * time.Hour, MaxMsgsPer: -1, MaxMsgSize: -1,
				Discard: server.DiscardOld, Duplicates: 2 * time.Minute}
		}
		for j := range perStream {
			c := &server.ConsumerInfo{
				Stream: s.Name, Name: fmt.Sprintf("worker-%d", j), Created: created.Add(time.Second),
				Delivered:  server.SequenceInfo{Consumer: uint64(i), Stream: uint64(i)},
				AckFloor:   server.SequenceInfo{Consumer: uint64(i), Stream: uint64(i)},
				NumPending: uint64(j), Cluster: cluster(fmt.Sprintf("C-%d-%d", i, j)), TimeStamp: at,
			}
			if configs {
				c.Config = &server.ConsumerConfig{Durable: c.Name, DeliverPolicy: server.DeliverAll,
					AckPolicy: server.AckExplicit, AckWait: 30 * time.Second, MaxDeliver: -1,
					FilterSubject: fmt.Sprintf("orders.%d.%d", i, j), ReplayPolicy: server.ReplayInstant,
					MaxWaiting: 512, MaxAckPending: 1000}
			}
			s.Consumer = append(s.Consumer, c)
		}
		acc.Streams = append(acc.Streams, s)
	}
	return &collect.ServerView{
		Info: server.ServerInfo{ID: id, Name: name},
		Jsz:  &server.JSInfo{AccountDetails: []*server.AccountDetail{acc}},
	}
}

// TestJetStreamWriteTime logs how long one epoch's stream and consumer
// rows take to write: 1,000 R3 streams of 2 consumers each, reported by
// the 3 servers that hold them, inside the epoch's transaction of a file
// store. It times the write without configurations (stats and replica
// rows only) and with them; the difference is the configuration versions.
// The first round with configurations starts every version, later rounds
// extend them. The identity rows (streams, consumers, raft_groups) are
// timed apart.
func TestJetStreamWriteTime(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	const streams, perStream = 1000, 2
	peers := []string{"nats-0", "nats-1", "nats-2"}
	for _, configs := range []bool{false, true} {
		ctx := context.Background()
		s, err := Open(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		for round := range 3 {
			epoch := int64(round + 1)
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			w := newWriter(ctx, tx, epoch, collect.NewSnapshot(t0))
			w.prevEpoch = epoch - 1
			views := make([]*collect.ServerView, len(peers))
			for i, p := range peers {
				views[i] = jszView(fmt.Sprintf("NSERVER%d", i), p, peers, streams, perStream, t0, configs)
			}
			began := time.Now()
			for _, v := range views {
				w.writeJetStream(v)
			}
			took := time.Since(began)
			began = time.Now()
			w.writeIdents()
			w.closeStatements()
			idents := time.Since(began)
			if w.err != nil {
				t.Fatal(w.err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			t.Logf("configs %v, round %d: %d streams and %d consumers on %d servers written in %v, their identity rows in %v",
				configs, round, streams, streams*perStream, len(peers), took, idents)
		}
		db := s.DB()
		want := map[string]int{
			"stream_stats": 3 * 3 * streams, "consumer_stats": 3 * 3 * streams * perStream,
			"stream_replicas": 3 * 3 * 2 * streams, "consumer_replicas": 3 * 3 * 2 * streams * perStream,
		}
		if configs {
			want["stream_config"], want["consumer_config"] = 3*streams, 3*streams*perStream
		}
		for table, n := range want {
			if got := count(t, db, `SELECT count(*) FROM `+table); got != n {
				t.Errorf("%s rows = %d, want %d", table, got, n)
			}
		}
		if configs {
			if got := count(t, db, `SELECT count(*) FROM stream_config WHERE first_epoch = 1 AND last_epoch = 3`); got != 3*streams {
				t.Errorf("stream_config runs over epochs 1 to 3 = %d, want %d", got, 3*streams)
			}
		}
		_ = s.Close()
	}
}
