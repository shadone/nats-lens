package checks_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/runner"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

const inventoryYAML = `accounts:
  - name: APP
    streams:
      - name: order-jobs
        replicas: 3
        consumers:
          - name: resize-worker
            max_deliver: 3
`

// A durable from the inventory deleted by a named client fires
// consumer-010 with the delete event as its cause, and resolves when the
// durable is back; a stopped server fires server-001 until it answers
// again.
func TestChecksAgainstCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := testcluster.Start(t, 3)
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	invPath := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(invPath, []byte(inventoryYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory.Load(invPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, rows, invPath, time.Now()); err != nil {
		t.Fatal(err)
	}

	capture := events.New(events.Options{Writer: st, FlushInterval: 100 * time.Millisecond, Log: log})
	if err := capture.SubscribeSys(c.Connect(0, testcluster.SysUser, testcluster.SysPass)); err != nil {
		t.Fatal(err)
	}
	app := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	if err := capture.SubscribeAccount("APP", app); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = capture.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	js, err := jetstream.New(app)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "order-jobs", Subjects: []string{"orders.>"}, Replicas: 3})
	if err != nil {
		t.Fatal(err)
	}
	durable := jetstream.ConsumerConfig{Durable: "resize-worker", AckPolicy: jetstream.AckExplicitPolicy, MaxDeliver: 3}
	if _, err := stream.CreateOrUpdateConsumer(ctx, durable); err != nil {
		t.Fatal(err)
	}

	set, err := checks.Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{
		Collector: collect.New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), collect.Options{
			Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour), Log: log,
		}),
		Store: st, Interval: 10 * time.Second, Retention: time.Hour, Log: log,
		Checks: &checks.Engine{Set: set, Log: log},
	}
	epoch := func() int64 {
		t.Helper()
		e, err := r.Once(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	finding := func(id string) store.Finding {
		t.Helper()
		f, err := st.Finding(ctx, id, time.Now())
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return f
	}
	const durableID = "consumer-010:APP/order-jobs/resize-worker"

	// A healthy cluster raises nothing above info (the stream has no
	// limits: stream-005).
	e1 := epoch()
	healthy, err := st.Findings(ctx, time.Now(), store.FindingQuery{FiringOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range healthy {
		if f.Severity != "info" {
			t.Errorf("healthy cluster: %s %s %s", f.ID, f.Severity, f.Detail)
		}
	}

	deleter, err := nats.Connect(c.URL(1), nats.UserInfo(testcluster.AppUser, testcluster.AppPass), nats.Name("test-deleter"))
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Close()
	djs, err := jetstream.New(deleter)
	if err != nil {
		t.Fatal(err)
	}
	if err := djs.DeleteConsumer(ctx, "order-jobs", "resize-worker"); err != nil {
		t.Fatal(err)
	}
	var deleteID int64
	deadline := time.Now().Add(20 * time.Second)
	for deleteID == 0 {
		_ = st.DB().QueryRow(`SELECT id FROM events WHERE kind = 'consumer.deleted' AND consumer = 'resize-worker'
AND client_name = 'test-deleter'`).Scan(&deleteID)
		if deleteID == 0 && time.Now().After(deadline) {
			t.Fatal("no consumer.deleted event naming test-deleter")
		}
		time.Sleep(100 * time.Millisecond)
	}

	e2 := epoch()
	f := finding(durableID)
	if !f.Firing() || f.Severity != "critical" || f.CauseEventID != deleteID || f.SinceEpoch != e2 ||
		!strings.Contains(f.Detail, "by test-deleter (127.0.0.1)") {
		t.Fatalf("after the delete (event %d): %+v", deleteID, f)
	}
	if f.RelatedSource != "inventory" || f.RelatedConfig["max_deliver"] != "3" {
		t.Errorf("related config = %v from %q", f.RelatedConfig, f.RelatedSource)
	}

	if _, err := stream.CreateOrUpdateConsumer(ctx, durable); err != nil {
		t.Fatal(err)
	}
	e3 := epoch()
	f = finding(durableID)
	if f.Firing() || !f.ResolvedAt.After(f.Since) {
		t.Fatalf("after recreating: %+v", f)
	}
	ts, err := st.FindingTransitions(ctx, store.TransitionQuery{FindingID: durableID})
	if err != nil || len(ts) != 2 || ts[0].Kind != store.TransitionResolved || ts[0].Epoch != e3 ||
		ts[1].Kind != store.TransitionFiring || ts[1].Epoch != e2 {
		t.Fatalf("transitions = %+v, %v (epochs %d %d %d)", ts, err, e1, e2, e3)
	}

	c.Stop(2)
	epoch()
	f = finding("server-001:nats-2")
	// A clean stop sends a shutdown event, so the server is known to be
	// stopping on purpose: info, for restart_window.
	if !f.Firing() || f.Severity != "info" || !strings.Contains(f.Detail, "announced shutdown") {
		t.Fatalf("with nats-2 stopped: %+v", f)
	}
	c.Restart(2)
	// The restarted server answers once it has rejoined; give it a few
	// epochs.
	for i := 0; ; i++ {
		epoch()
		if f = finding("server-001:nats-2"); !f.Firing() {
			break
		}
		if i == 10 {
			t.Fatalf("server-001 still firing after the restart: %+v", f)
		}
		time.Sleep(500 * time.Millisecond)
	}
	var failed int
	if err := st.DB().QueryRow(`SELECT count(*) FROM check_runs WHERE status <> 'ok'`).Scan(&failed); err != nil || failed != 0 {
		var msg string
		_ = st.DB().QueryRow(`SELECT string_agg(check_code || ': ' || error, '; ') FROM check_runs WHERE status <> 'ok'`).Scan(&msg)
		t.Fatalf("%d check runs failed: %s %v", failed, msg, err)
	}
	all, _ := st.FindingTransitions(ctx, store.TransitionQuery{})
	for i := len(all) - 1; i >= 0; i-- {
		t.Logf("epoch %d %s %s %s: %s", all[i].Epoch, all[i].Kind, all[i].FindingID, all[i].Severity, all[i].Detail)
	}
	if restart := finding("server-004:nats-2"); !restart.Firing() {
		t.Errorf("server-004 after the restart: %+v", restart)
	}
}
