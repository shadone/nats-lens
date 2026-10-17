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

const rollingInventory = `accounts:
  - name: APP
    streams:
      - name: s3
        replicas: 3
        consumers:
          - name: d3
      - name: s1
        replicas: 1
        consumers:
          - name: d1
`

// allowedDuringRestart says whether a finding may fire, at its severity,
// while a healthy cluster restarts one server at a time with lame duck:
// info always (a replica offline because its server restarts is info); a
// replica really behind (stream-003) at warning.
func allowedDuringRestart(code, severity string) bool {
	switch {
	case severity == "info":
		return true
	case code == "stream-003":
		return severity == "warning"
	}
	return false
}

// A rolling restart of a healthy 3-node cluster, each server through lame
// duck, raises nothing above info except replicas behind while they are. This is the acceptance test of the restart rules.
func TestRollingRestartRaisesNothing(t *testing.T) {
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
	invPath := filepath.Join(t.TempDir(), "inv.yaml")
	if err := os.WriteFile(invPath, []byte(rollingInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory.Load(invPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, rows, invPath, time.Now()); err != nil {
		t.Fatal(err)
	}
	urls := strings.Join([]string{c.URL(0), c.URL(1), c.URL(2)}, ",")
	dial := func(user, pass string) *nats.Conn {
		nc, err := nats.Connect(urls, nats.UserInfo(user, pass), nats.MaxReconnects(-1), nats.ReconnectWait(100*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nc.Close)
		return nc
	}
	capture := events.New(events.Options{Writer: st, FlushInterval: 100 * time.Millisecond, Log: log})
	if err := capture.SubscribeSys(dial(testcluster.SysUser, testcluster.SysPass)); err != nil {
		t.Fatal(err)
	}
	app := dial(testcluster.AppUser, testcluster.AppPass)
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
	for _, a := range []struct {
		stream, durable string
		replicas        int
	}{{"s3", "d3", 3}, {"s1", "d1", 1}} {
		s, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: a.stream, Subjects: []string{a.stream + ".>"},
			Replicas: a.replicas, Retention: jetstream.WorkQueuePolicy})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: a.durable,
			AckPolicy: jetstream.AckExplicitPolicy, MaxDeliver: 3}); err != nil {
			t.Fatal(err)
		}
		for range 5 {
			if _, err := js.Publish(ctx, a.stream+".x", []byte("x")); err != nil {
				t.Fatal(err)
			}
		}
	}
	set, err := checks.Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{
		Collector: collect.New(dial(testcluster.SysUser, testcluster.SysPass), collect.Options{
			Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour), Log: log}),
		Store: st, Interval: 10 * time.Second, Retention: time.Hour, Log: log,
		Checks: &checks.Engine{Set: set, Log: log},
	}
	epoch := func() {
		t.Helper()
		// Let the last events of the step be written before the epoch.
		time.Sleep(300 * time.Millisecond)
		if _, err := r.Once(ctx); err != nil {
			t.Fatal(err)
		}
	}
	epoch()
	for i := range 3 {
		c.LameDuck(i)
		epoch()
		c.RestartAsync(i)
		for range 8 {
			epoch()
			time.Sleep(700 * time.Millisecond)
		}
		c.WaitMeta()
		epoch()
	}
	ts, err := st.FindingTransitions(ctx, store.TransitionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(ts) - 1; i >= 0; i-- {
		tr := ts[i]
		t.Logf("epoch %d %s %s %s: %s", tr.Epoch, tr.Kind, tr.FindingID, tr.Severity, tr.Detail)
		if tr.Kind != store.TransitionResolved && !allowedDuringRestart(tr.CheckCode, tr.Severity) {
			t.Errorf("epoch %d: %s %s during a rolling restart: %s", tr.Epoch, tr.FindingID, tr.Severity, tr.Detail)
		}
	}
	var failed int
	if err := st.DB().QueryRow(`SELECT count(*) FROM check_runs WHERE status <> 'ok'`).Scan(&failed); err != nil || failed != 0 {
		t.Errorf("%d check runs failed (%v)", failed, err)
	}
}
