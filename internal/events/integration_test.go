package events_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// waitRow polls until the query returns a row, and returns its columns.
func waitRow(t *testing.T, db *sql.DB, what, q string, args ...any) []string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		rows, err := db.Query(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		cols, _ := rows.Columns()
		if rows.Next() {
			vals := make([]sql.NullString, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			out := make([]string, len(vals))
			for i, v := range vals {
				out[i] = v.String
			}
			return out
		}
		rows.Close()
		if time.Now().After(deadline) {
			dumpEvents(t, db)
			t.Fatalf("no %s", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func dumpEvents(t *testing.T, db *sql.DB) {
	rows, err := db.Query(`SELECT kind || ' ' || source || ' ' || coalesce(json_extract_string(raw, '$.id'), ''), coalesce(server_name, ''), coalesce(stream, ''), coalesce(consumer, ''), summary FROM events ORDER BY id`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var k, s, st, c, sum string
		_ = rows.Scan(&k, &s, &st, &c, &sum)
		t.Logf("event %s server=%s stream=%s consumer=%s: %s", k, s, st, c, sum)
	}
}

func TestCaptureFromCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testcluster.Start(t, 3)
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db := st.DB()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	capture := events.New(events.Options{Writer: st, FlushInterval: 100 * time.Millisecond, Log: log})
	if err := capture.SubscribeSys(c.Connect(0, testcluster.SysUser, testcluster.SysPass)); err != nil {
		t.Fatal(err)
	}
	app := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	// The label differs from the account name; events carry the name.
	if err := capture.SubscribeAccount("app-label", app); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = capture.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	// A collector connection on nats-2 that will lose its server.
	urls := strings.Join([]string{c.URL(2), c.URL(0), c.URL(1)}, ",")
	probe, err := nats.Connect(urls, append([]nats.Option{
		nats.UserInfo(testcluster.SysUser, testcluster.SysPass), nats.DontRandomize(),
		nats.ReconnectWait(100 * time.Millisecond), nats.MaxReconnects(-1),
	}, capture.ConnHandlers("probe")...)...)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()

	js, err := jetstream.New(app)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "order-jobs", Subjects: []string{"orders.>"}, Replicas: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"resize-worker", "index-worker"} {
		if _, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: name, AckPolicy: jetstream.AckExplicitPolicy}); err != nil {
			t.Fatal(err)
		}
	}
	moveLeaders(t, app, "order-jobs", "resize-worker", "nats-2")

	t.Run("consumer deleted by a named client", func(t *testing.T) {
		deleter, err := nats.Connect(c.URL(1), nats.UserInfo(testcluster.AppUser, testcluster.AppPass), nats.Name("test-deleter"))
		if err != nil {
			t.Fatal(err)
		}
		defer deleter.Close()
		djs, _ := jetstream.New(deleter)
		if err := djs.DeleteConsumer(ctx, "order-jobs", "index-worker"); err != nil {
			t.Fatal(err)
		}
		row := waitRow(t, db, "consumer.deleted event", `SELECT account, client_name, client_host, client_user, summary FROM events
WHERE kind = 'consumer.deleted' AND stream = 'order-jobs' AND consumer = 'index-worker'`)
		if row[0] != "APP" || row[1] != "test-deleter" || row[2] != "127.0.0.1" || row[3] != "app" {
			t.Fatalf("deleted event = %v", row)
		}
		if !strings.Contains(row[4], "test-deleter") {
			t.Errorf("summary = %q", row[4])
		}
	})

	t.Run("client disconnect with reason", func(t *testing.T) {
		victim, err := nats.Connect(c.URL(1), nats.UserInfo(testcluster.AppUser, testcluster.AppPass), nats.Name("victim"))
		if err != nil {
			t.Fatal(err)
		}
		victim.Close()
		row := waitRow(t, db, "client.disconnect event", `SELECT server_name, account, reason FROM events
WHERE kind = 'client.disconnect' AND client_name = 'victim'`)
		if row[0] != "nats-1" || row[1] != "APP" || row[2] != "Client Closed" {
			t.Fatalf("disconnect = %v", row)
		}
	})

	t.Run("failed login recorded once per server", func(t *testing.T) {
		if bad, err := nats.Connect(c.URL(1), nats.UserInfo(testcluster.AppUser, "wrong"), nats.Name("intruder"),
			nats.MaxReconnects(0)); err == nil {
			bad.Close()
		}
		waitRow(t, db, "client.auth_error event", `SELECT server_name FROM events WHERE kind = 'client.auth_error' AND client_name = 'intruder'`)
		time.Sleep(300 * time.Millisecond)
		// The client tries each server it learned of, so there is one
		// failure per server; none may be stored twice.
		var n int
		if err := db.QueryRow(`SELECT count(*) - count(DISTINCT server_name || json_extract_string(raw, '$.client.id'))
FROM events WHERE kind = 'client.auth_error' AND client_name = 'intruder'`).Scan(&n); err != nil || n != 0 {
			t.Errorf("auth errors stored twice: %d, %v", n, err)
		}
	})

	t.Run("server stop elects new leaders", func(t *testing.T) {
		c.LameDuck(2)
		for _, q := range []struct{ what, sql string }{
			{"stream.leader_elected", `SELECT server_name FROM events WHERE kind = 'stream.leader_elected'
AND stream = 'order-jobs' AND server_name <> 'nats-2'`},
			{"consumer.leader_elected", `SELECT server_name FROM events WHERE kind = 'consumer.leader_elected'
AND stream = 'order-jobs' AND consumer = 'resize-worker' AND server_name <> 'nats-2'`},
			{"lameduck or shutdown", `SELECT kind FROM events WHERE kind IN ('server.lameduck', 'server.shutdown') AND server_name = 'nats-2'`},
			{"collector.disconnected", `SELECT reason FROM events WHERE kind = 'collector.disconnected' AND json_extract_string(raw, '$.connection') = 'probe'`},
			{"collector.reconnected", `SELECT server_name FROM events WHERE kind = 'collector.reconnected' AND server_name <> 'nats-2'`},
		} {
			row := waitRow(t, db, q.what, q.sql)
			t.Logf("%s: %v", q.what, row)
		}
	})

	// A deliberate close of a collector connection is not a loss.
	probe.Close()
	time.Sleep(300 * time.Millisecond)
	var closed int
	if err := db.QueryRow(`SELECT count(*) FROM events WHERE kind IN ('collector.connection_closed', 'collector.disconnected')
AND json_extract_string(raw, '$.connection') = 'probe' AND received_at > (SELECT max(received_at) FROM events WHERE kind = 'collector.reconnected')`).Scan(&closed); err != nil || closed != 0 {
		t.Errorf("deliberate close recorded: %d %v", closed, err)
	}

	if t.Failed() {
		dumpEvents(t, db)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM events WHERE kind IN ('api.audit', 'account.connections')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("api audits or account connection counts stored while off: %d %v", n, err)
	}
	// Cluster advisories reach both the account and the system account
	// subscription; only one copy is stored.
	if err := db.QueryRow(`SELECT count(*) - count(DISTINCT json_extract_string(raw, '$.id'))
FROM events WHERE source = 'jetstream'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("duplicate advisories stored: %d %v", n, err)
	}
}
