package main

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// An events connection that nats.go closed after rejected credentials is
// dialled again, and the redial is stored as collector.reconnected of
// that connection, so that Replay's time without events ends.
func TestEventsConnectionRedialIsRecorded(t *testing.T) {
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

	cfg := config.NATS{URL: c.URL(0), Name: "natslens-events", User: testcluster.SysUser, Password: testcluster.SysPass}
	ec := newEventsConn(capture, "sys", cfg, capture.SubscribeSys, quietLog())
	ec.backoff = redialBackoff{first: 200 * time.Millisecond, max: 500 * time.Millisecond, reset: time.Minute}
	if err := ec.open(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if nc := ec.current(); nc != nil {
			nc.Close()
		}
	}()
	go ec.keep(ctx, 50*time.Millisecond)
	waitFor(t, "first connect", 5*time.Second, func() bool { return ec.current().IsConnected() })

	first := ec.current()
	c.SetSysPassword(testcluster.SysPass, "rotated-elsewhere")
	waitFor(t, "the connection to close for good", 20*time.Second, first.IsClosed)
	c.SetSysPassword("rotated-elsewhere", testcluster.SysPass)
	waitFor(t, "a new connection", 20*time.Second, func() bool {
		nc := ec.current()
		return nc != first && nc.IsConnected()
	})
	kinds := func(kind string) int {
		var n int
		if err := st.DB().QueryRow(`SELECT count(*) FROM events WHERE kind = ? AND (raw->>'connection') = 'sys'`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	waitFor(t, "the closed and reconnected events of sys", 5*time.Second, func() bool {
		return kinds("collector.connection_closed") >= 1 && kinds("collector.reconnected") >= 1
	})
}
