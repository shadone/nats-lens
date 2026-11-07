package runner

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/notify"
)

// Each epoch whose checks were applied is reconciled after them, and the
// retention sweep sweeps the notifications; an epoch without checks is
// not reconciled.
func TestOnceQueuesNotifications(t *testing.T) {
	r := newRunner(t)
	ctx := context.Background()
	var called []int64
	r.Notify = func(context.Context, int64) error { t.Fatal("notified without checks"); return nil }
	if _, err := r.Once(ctx); err != nil {
		t.Fatal(err)
	}
	set, err := checks.Load(config.Checks{Timeout: config.Duration(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	r.Checks = &checks.Engine{Set: set}
	ch := config.DefaultChannel()
	ch.Name, ch.Type, ch.URL = "ops", config.ChannelWebhook, "https://hooks.example.com/ops"
	rec := &notify.Reconciler{Store: r.Store, SystemID: "example-prod", Channels: []config.Channel{ch}, Checks: set, Retention: time.Hour}
	r.Notify = func(ctx context.Context, epoch int64) error {
		var runs int
		if err := r.Store.DB().QueryRow(`SELECT count(*) FROM check_runs WHERE epoch = ?`, epoch).Scan(&runs); err != nil || runs == 0 {
			t.Errorf("epoch %d reconciled before its checks were stored (%d runs, %v)", epoch, runs, err)
		}
		called = append(called, epoch)
		_, err := rec.Reconcile(ctx, epoch)
		return err
	}
	var swept int
	r.NotifySweep = func(ctx context.Context, now time.Time) error {
		swept++
		return rec.Sweep(ctx, now)
	}
	epoch, err := r.Once(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(called) != 1 || called[0] != epoch {
		t.Fatalf("notify called with %v, want [%d]", called, epoch)
	}
	rows, err := r.Store.PendingOutbox(ctx, "ops", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Kind != string(notify.KindBaseline) || rows[0].Epoch != epoch {
		t.Fatalf("outbox after the first epoch: %+v", rows)
	}
	r.sweep(ctx)
	if swept != 1 {
		t.Errorf("notify sweep ran %d times with one retention sweep", swept)
	}
}
