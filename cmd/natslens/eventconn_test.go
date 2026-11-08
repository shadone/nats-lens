package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// A connection nats.go closed is dialled and subscribed again.
func TestEventConnRedials(t *testing.T) {
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
	var dials, subscribed atomic.Int64
	ec := &eventConn{
		label: "test", log: quietLog(),
		dial: func() (*nats.Conn, error) {
			dials.Add(1)
			return nats.Connect(s.ClientURL())
		},
		subscribe: func(nc *nats.Conn) error {
			subscribed.Add(1)
			_, err := nc.Subscribe("x", func(*nats.Msg) {})
			return err
		},
	}
	if err := ec.open(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { ec.keep(ctx, 10*time.Millisecond); close(done) }()
	ec.current().Close()
	deadline := time.Now().Add(redialFirst + 3*time.Second)
	for subscribed.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if dials.Load() != 2 || subscribed.Load() != 2 {
		t.Fatalf("dials %d, subscriptions %d; want a second of each", dials.Load(), subscribed.Load())
	}
	if !ec.current().IsConnected() {
		t.Fatal("redialled connection not connected")
	}
	ec.drain(time.Second)
	if !ec.current().IsClosed() {
		t.Fatal("drain left the connection open")
	}
}
