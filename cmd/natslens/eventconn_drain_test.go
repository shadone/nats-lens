package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// A drain hands every message already received to the handler before the
// connection closes. From the review.
func TestDrainDeliversBacklog(t *testing.T) {
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
	s, _ := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	go s.Start()
	defer s.Shutdown()
	s.ReadyForConnections(5 * time.Second)
	var got atomic.Int64
	gate := make(chan struct{})
	ec := &eventConn{label: "t", log: quietLog(),
		dial: func() (*nats.Conn, error) { return nats.Connect(s.ClientURL()) },
		subscribe: func(nc *nats.Conn) error {
			_, err := nc.Subscribe("x", func(*nats.Msg) { <-gate; got.Add(1) })
			return err
		}}
	if err := ec.open(); err != nil {
		t.Fatal(err)
	}
	pub, _ := nats.Connect(s.ClientURL())
	for i := 0; i < 100; i++ {
		pub.Publish("x", []byte("m"))
	}
	pub.Flush()
	time.Sleep(100 * time.Millisecond)
	go func() { time.Sleep(200 * time.Millisecond); close(gate) }()
	ec.drain(5 * time.Second)
	if got.Load() != 100 {
		t.Fatalf("handled %d of 100", got.Load())
	}
}

// Connections drain in parallel: when each one hits the drain timeout,
// shutdown waits one timeout, not one per connection.
func TestDrainAllInParallel(t *testing.T) {
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
	s, _ := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	go s.Start()
	defer s.Shutdown()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	release := make(chan struct{})
	defer close(release)
	newConn := func(subject string) *eventConn {
		ec := &eventConn{label: subject, log: quietLog(),
			dial: func() (*nats.Conn, error) { return nats.Connect(s.ClientURL()) },
			subscribe: func(nc *nats.Conn) error {
				_, err := nc.Subscribe(subject, func(*nats.Msg) { <-release })
				return err
			}}
		if err := ec.open(); err != nil {
			t.Fatal(err)
		}
		return ec
	}
	conns := []*eventConn{newConn("a"), newConn("b"), newConn("c")}
	pub, err := nats.Connect(s.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	for _, subj := range []string{"a", "b", "c"} {
		_ = pub.Publish(subj, nil)
	}
	_ = pub.Flush()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	drainAll(conns, 200*time.Millisecond)
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Errorf("draining three stuck connections took %s, want about one timeout (200ms)", took)
	}
}
