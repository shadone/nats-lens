package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// A state's since is kept while the state stays; the newest error is kept
// after a reconnect; a connection closed for good stays closed while it is
// dialled again, until it connects.
func TestCollectionStatusTransitions(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s := newCollectionStatus(func() time.Time { return now })
	if got := s.get(); got.State != api.CollectionStarting || !got.Since.Equal(now) {
		t.Fatalf("new status %+v", got)
	}
	now = now.Add(time.Second)
	s.set(api.CollectionNotConnected, errors.New("connection refused"))
	now = now.Add(time.Second)
	s.set(api.CollectionNotConnected, errors.New("i/o timeout"))
	got := s.get()
	if got.State != api.CollectionNotConnected || got.LastError != "i/o timeout" || !got.Since.Equal(now.Add(-time.Second)) {
		t.Fatalf("not connected twice: %+v", got)
	}
	s.set(api.CollectionConnectionClosed, errors.New("authorization violation"))
	s.set(api.CollectionNotConnected, errors.New("authorization violation again"))
	if got := s.get(); got.State != api.CollectionConnectionClosed || got.LastError != "authorization violation again" {
		t.Fatalf("redial after a close: %+v", got)
	}
	now = now.Add(time.Second)
	s.set(api.CollectionCollecting, nil)
	at := now.Add(-time.Minute)
	s.epoch(at)
	got = s.get()
	if got.State != api.CollectionCollecting || !got.Since.Equal(now) || got.LastError != "authorization violation again" ||
		!got.LastEpochAt.Equal(at) {
		t.Fatalf("collecting: %+v", got)
	}
}

// The collector connection of a system whose NATS is unreachable reports
// not_connected, and collecting once the server comes up; the capture's
// handlers still record the connect as an event.
func TestCollectorConnFeedsStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	port := testcluster.FreePort(t)
	cfg := config.NATS{URL: fmt.Sprintf("nats://127.0.0.1:%d", port), Name: "natslens-shop-staging",
		User: testcluster.SysUser, Password: testcluster.SysPass}
	col := collect.New(nil, collect.Options{Window: 300 * time.Millisecond, ConnzLimit: 10,
		Tracker: collect.NewTracker(time.Hour), Log: quietLog()})
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	capture := events.New(events.Options{Writer: st, FlushInterval: 100 * time.Millisecond, Log: quietLog()})
	cctx, stopCapture := context.WithCancel(ctx)
	captured := make(chan struct{})
	go func() { _ = capture.Run(cctx); close(captured) }()
	defer func() { stopCapture(); <-captured }()
	status := newCollectionStatus(nil)
	cc := newTrackedCollectorConn(cfg, capture, col, status, quietLog())
	if err := cc.open(); err != nil {
		t.Fatal(err)
	}
	defer func() { cc.current().Close() }()
	waitFor(t, "not_connected", 10*time.Second, func() bool { return status.get().State == api.CollectionNotConnected })
	if status.get().LastError == "" {
		t.Error("not_connected without an error")
	}
	testcluster.StartOnPort(t, port)
	waitFor(t, "collecting", 20*time.Second, func() bool { return status.get().State == api.CollectionCollecting })
	waitFor(t, "the connect event", 5*time.Second, func() bool {
		var n int
		if err := st.DB().QueryRow(`SELECT count(*) FROM events WHERE kind = 'collector.reconnected'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	})
}
