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
