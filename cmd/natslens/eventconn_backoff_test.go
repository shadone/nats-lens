package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// A failing dial keeps backing off even after the first connection lasted
// longer than the reset time. Adapted from the review's test.
func TestRedialBacksOffWhenDialFails(t *testing.T) {
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
	var mu sync.Mutex
	var attempts []time.Time
	first := true
	ec := &eventConn{label: "t", log: quietLog(),
		backoff: redialBackoff{first: 20 * time.Millisecond, max: 400 * time.Millisecond, reset: 100 * time.Millisecond},
		dial: func() (*nats.Conn, error) {
			mu.Lock()
			defer mu.Unlock()
			if first {
				first = false
				return nats.Connect(s.ClientURL())
			}
			attempts = append(attempts, time.Now())
			return nil, errors.New("creds unreadable")
		},
		subscribe: func(*nats.Conn) error { return nil }}
	if err := ec.open(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { ec.keep(ctx, 5*time.Millisecond); close(done) }()
	time.Sleep(300 * time.Millisecond) // longer than reset
	ec.current().Close()
	<-done
	mu.Lock()
	defer mu.Unlock()
	// 20, 40, 80, 160, 320, then 400 ms steps fit about 7 attempts in 1.7s;
	// without backoff every attempt waits 20ms (about 80).
	if len(attempts) > 12 {
		t.Fatalf("no backoff: %d dial attempts in 1.7s", len(attempts))
	}
	if len(attempts) < 3 {
		t.Fatalf("only %d dial attempts", len(attempts))
	}
}
