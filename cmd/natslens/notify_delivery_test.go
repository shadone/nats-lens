package main

import (
	"context"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/notify"
	"github.com/shadone/nats-lens/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestDeliveryShutdownDeadline(t *testing.T) {
	st, err := store.OpenPath(context.Background(), filepath.Join(t.TempDir(), "delivery.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, _ := notify.BaselineMessage("example-prod", 0, 1).Encode()
	if err := st.ApplyNotify(context.Background(), store.NotifyChange{Outbox: []store.OutboxRow{{Channel: "ops", CreatedAt: time.Now(), Epoch: 1, Kind: "baseline", DedupKey: "baseline", Message: b}}}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release }))
	defer srv.Close()
	defer close(release)
	ch := config.DefaultChannel()
	ch.Name = "ops"
	ch.Type = config.ChannelWebhook
	ch.URL = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := startNotificationDelivery(ctx, &notify.Delivery{Store: st, SystemID: "example-prod", Channels: []config.Channel{ch}}, 50*time.Millisecond)
	<-started
	at := time.Now()
	cancel()
	stop()
	elapsed := time.Since(at)
	if elapsed < 40*time.Millisecond || elapsed > time.Second {
		t.Fatalf("shutdown grace: %s", elapsed)
	}
	rows, _ := st.OutboxRows(context.Background(), "ops")
	if rows[0].State != "pending" {
		t.Fatal(rows)
	}
}

// All systems share the signal's deadline: waiting for one system must not
// give each subsequent system a fresh grace period.
func TestDeliveryMultiSystemShutdownDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-release }))
	defer srv.Close()
	defer close(release)
	var stops []func()
	var stores []*store.Store
	const grace = 200 * time.Millisecond
	for _, id := range []string{"example-prod", "example-staging", "example-dev"} {
		st, err := store.OpenPath(context.Background(), filepath.Join(t.TempDir(), "delivery.duckdb"))
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		stores = append(stores, st)
		b, _ := notify.BaselineMessage(id, 0, 1).Encode()
		if err := st.ApplyNotify(ctx, store.NotifyChange{Outbox: []store.OutboxRow{{Channel: "ops", CreatedAt: time.Now(), Epoch: 1, Kind: "baseline", DedupKey: "baseline", Message: b}}}); err != nil {
			t.Fatal(err)
		}
		ch := config.DefaultChannel()
		ch.Name = "ops"
		ch.Type = config.ChannelWebhook
		ch.URL = srv.URL
		stops = append(stops, startNotificationDelivery(ctx, &notify.Delivery{Store: st, SystemID: id, Channels: []config.Channel{ch}}, grace))
	}
	for range stores {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("delivery did not start")
		}
	}
	at := time.Now()
	cancel()
	for _, stop := range stops {
		stop()
	}
	if elapsed := time.Since(at); elapsed < grace*8/10 || elapsed >= 2*grace {
		t.Fatalf("combined shutdown grace: %s", elapsed)
	}
	for _, st := range stores {
		rows, err := st.OutboxRows(context.Background(), "ops")
		if err != nil || len(rows) != 1 || rows[0].State != "pending" || rows[0].Attempts != 0 {
			t.Fatalf("shutdown changed pending attempt: %+v, %v", rows, err)
		}
	}
}
