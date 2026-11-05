package main

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// natslens run sweeps the notification rows also without channels, so a
// channel removed from the file and added again later starts from a
// baseline; once has no hooks.
func TestNotifyHooksSweepWithoutChannels(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	ch := config.DefaultChannel()
	ch.Name, ch.Type, ch.URL = "ops", config.ChannelWebhook, "https://hooks.example.com/ops"
	rt := func(chans ...config.Channel) *systemRuntime {
		cfg := config.Config{Retention: config.Duration(14 * 24 * time.Hour)}
		cfg.Notifications.Channels = chans
		return &systemRuntime{sys: config.System{ID: "example-prod"}, cfg: cfg, log: quietLog(), st: st}
	}
	at := time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	epoch := func(n int64) {
		t.Helper()
		e := at.Add(time.Duration(n) * time.Minute)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [], NULL)`, n, e, e); err != nil {
			t.Fatal(err)
		}
	}
	baselines := func() int {
		t.Helper()
		var n int
		if err := st.DB().QueryRow(`SELECT count(*) FROM notify_outbox WHERE kind = 'baseline'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n, s := rt(ch).notifyHooks("once", set); n != nil || s != nil {
		t.Fatal("natslens once has notification hooks")
	}
	notifyFn, _ := rt(ch).notifyHooks("run", set)
	epoch(1)
	if err := notifyFn(ctx, 1); err != nil || baselines() != 1 {
		t.Fatalf("first reconcile: %v, %d baselines", err, baselines())
	}
	none, sweep := rt().notifyHooks("run", set)
	if none != nil || sweep == nil {
		t.Fatalf("without channels: notify set %v, sweep set %v", none != nil, sweep != nil)
	}
	if err := sweep(ctx, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := st.DB().QueryRow(`SELECT (SELECT count(*) FROM notify_channels) + (SELECT count(*) FROM notify_outbox)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d rows of a removed channel after the sweep (%v)", left, err)
	}
	notifyFn, _ = rt(ch).notifyHooks("run", set)
	epoch(2)
	if err := notifyFn(ctx, 2); err != nil || baselines() != 1 {
		t.Fatalf("re-added channel: %v, %d baselines, want 1 new", err, baselines())
	}
}
