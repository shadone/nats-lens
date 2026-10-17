package main

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

func waitFor(t *testing.T, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The collector's connection is closed for good when its credentials are
// rejected (a rotation). It is dialled again with backoff, collection
// resumes once the credentials work again, and both changes are events.
func TestCollectorConnectionRedialledAfterAuthFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server and waits for nats.go to give up")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testcluster.Start(t, 1)
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	capture := events.New(events.Options{Writer: st, FlushInterval: 100 * time.Millisecond, Log: quietLog()})
	cctx, stopCapture := context.WithCancel(context.Background())
	captured := make(chan struct{})
	go func() { _ = capture.Run(cctx); close(captured) }()
	defer func() { stopCapture(); <-captured }()

	cfg := config.NATS{URL: c.URL(0), Name: "natslens", User: testcluster.SysUser, Password: testcluster.SysPass}
	col := collect.New(nil, collect.Options{Window: 300 * time.Millisecond, ConnzLimit: 10,
		Tracker: collect.NewTracker(time.Hour), Log: quietLog()})
	cc := newCollectorConn(cfg, capture, col, quietLog())
	cc.backoff = redialBackoff{first: 200 * time.Millisecond, max: 500 * time.Millisecond, reset: time.Minute}
	if err := cc.open(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if nc := cc.current(); nc != nil {
			nc.Close()
		}
	}()
	go cc.keep(ctx, 50*time.Millisecond)

	waitFor(t, "first connect", 5*time.Second, func() bool { return cc.current().IsConnected() })
	if got := len(col.Collect(ctx).Servers); got != 1 {
		t.Fatalf("servers before the rotation = %d", got)
	}

	first := cc.current()
	c.SetSysPassword(testcluster.SysPass, "rotated-elsewhere")
	waitFor(t, "the connection to close for good", 20*time.Second, first.IsClosed)
	if got := len(col.Collect(ctx).Servers); got != 0 {
		t.Fatalf("servers while the credentials are rejected = %d", got)
	}

	c.SetSysPassword("rotated-elsewhere", testcluster.SysPass)
	waitFor(t, "a new connection", 20*time.Second, func() bool {
		nc := cc.current()
		return nc != first && nc.IsConnected()
	})
	if got := len(col.Collect(ctx).Servers); got != 1 {
		t.Fatalf("servers after the credentials work again = %d", got)
	}

	kinds := func(kind string) int {
		var n int
		if err := st.DB().QueryRow(`SELECT count(*) FROM events WHERE kind = ? AND (raw->>'connection') = 'collector'`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	waitFor(t, "the closed and reconnected events", 5*time.Second, func() bool {
		return kinds("collector.connection_closed") >= 1 && kinds("collector.reconnected") >= 1
	})
}
