package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// All mutations here belong to a disposable, loopback-only test fixture.
// The collector continues to read only JSZ and never fetches messages.
func TestSourceHealthLiveJSZ(t *testing.T) {
	if testing.Short() {
		t.Skip("in-process cluster")
	}
	c := testcluster.Start(t, 3)
	nc := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.AddStream(&nats.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 3}); err != nil {
		t.Fatal(err)
	}
	// The legacy nats.go AddStream validates transforms against the runtime
	// source list by index, although nats-server emits that list in map order.
	// Use the fixture's create API directly to avoid that unrelated validation.
	payload, err := json.Marshal(server.StreamConfig{Name: "combined", Storage: server.FileStorage, Replicas: 3, Sources: []*server.StreamSource{
		{Name: "orders", FilterSubject: "orders.eu.>"},
		{Name: "orders", SubjectTransforms: []server.SubjectTransformConfig{{Source: "orders.us.>", Destination: "us.>"}}},
		{Name: "absent"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := nc.Request("$JS.API.STREAM.CREATE.combined", payload, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var created server.JSApiStreamCreateResponse
	if err = json.Unmarshal(response.Data, &created); err != nil || created.Error != nil {
		t.Fatalf("create: %v %+v", err, created.Error)
	}

	st := openMem(t)
	deadline := time.Now().Add(15 * time.Second)
	sawNever := false
	idleSince := time.Time{}
	for time.Now().Before(deadline) {
		snap := collect.NewSnapshot(time.Now())
		for i, srv := range c.Servers {
			z, err := srv.Jsz(&server.JSzOptions{Accounts: true, Streams: true, Config: true})
			if err != nil {
				t.Fatal(err)
			}
			mustApply(t, snap, collect.Jsz, envelope(t, c.Names[i], srv.ID(), z))
		}
		snap.FinishedAt = time.Now()
		e, err := st.WriteSnapshot(context.Background(), snap)
		if err != nil {
			t.Fatal(err)
		}
		healthy := count(t, st.DB(), `SELECT count(*) FROM stream_source_stats WHERE epoch=? AND stream='combined' AND name='orders' AND reported AND active_ns>=0 AND active_ns<2000000000 AND lag=0`, e)
		if count(t, st.DB(), `SELECT count(*) FROM stream_source_stats WHERE epoch=? AND stream='combined' AND name='absent' AND reported AND active_ns<0`, e) > 0 {
			sawNever = true
		}
		if healthy == 2 {
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) > 2500*time.Millisecond && sawNever {
				// No application message has been published: freshness is heartbeats.
				if n := count(t, st.DB(), `SELECT count(*) FROM stream_source_stats src JOIN stream_stats ss USING(epoch,server_id,account,stream) WHERE src.epoch=? AND src.reported AND NOT ss.is_leader`, e); n != 0 {
					t.Fatalf("follower source reports: %d", n)
				}
				t.Log("two filtered/transformed idle sources receive heartbeats; absent source reports never-heard; followers are not authoritative")
				return
			}
		} else {
			idleSince = time.Time{}
		}
		time.Sleep(100 * time.Millisecond)
	}
	rows, _ := st.DB().Query(`SELECT src.server_id, ss.is_leader, src.name, src.reported, coalesce(src.active_ns,-999), coalesce(src.error,''), src.config::VARCHAR FROM stream_source_stats src JOIN stream_stats ss USING(epoch,server_id,account,stream) WHERE src.epoch=(SELECT max(epoch) FROM epochs)`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var sid, name, desc, cfg string
			var lead, reported bool
			var active int64
			_ = rows.Scan(&sid, &lead, &name, &reported, &active, &desc, &cfg)
			t.Log(sid, lead, name, reported, active, desc, cfg)
		}
	}
	t.Fatalf("source state did not settle; saw never-heard origin=%v", sawNever)
}
