package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// A connection whose first dial failed is dialled by keep with backoff
// until it opens, with one warning before the first dial and one per
// failed dial, and no error lines.
func TestKeepDialsAConnectionWhoseFirstDialFailed(t *testing.T) {
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	defer s.Shutdown()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	var dials atomic.Int32
	var buf bytes.Buffer
	ec := &eventConn{label: "t", log: slog.New(slog.NewTextHandler(&buf, nil)),
		backoff: redialBackoff{first: 20 * time.Millisecond, max: 100 * time.Millisecond, reset: time.Second},
		dial: func() (*nats.Conn, error) {
			if dials.Add(1) <= 2 {
				return nil, errors.New("creds unreadable")
			}
			return nats.Connect(s.ClientURL())
		}}
	if err := ec.open(); err == nil {
		t.Fatal("first dial succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { ec.keep(ctx, 5*time.Millisecond); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for ec.current() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	nc := ec.current()
	if nc == nil || !nc.IsConnected() {
		t.Fatalf("not dialled again after %d dials", dials.Load())
	}
	nc.Close()
	out := buf.String()
	if strings.Count(out, "connection not open; dialling") != 1 || strings.Count(out, "connection not opened; dialling again") != 1 ||
		strings.Contains(out, "level=ERROR") || strings.Contains(out, "connection closed") {
		t.Errorf("log:\n%s", out)
	}
}
