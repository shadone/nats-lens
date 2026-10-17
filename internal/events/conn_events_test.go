package events

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func kinds(w *flakyWriter) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, e := range w.evs {
		out = append(out, e.Kind)
	}
	return out
}

func waitKind(t *testing.T, w *flakyWriter, kind string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, k := range kinds(w) {
			if k == kind {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no %s event; got %v", kind, kinds(w))
}

// A connection that never reaches a server is recorded as not connected.
func TestNotConnectedAtStart(t *testing.T) {
	w := &flakyWriter{}
	c := New(Options{Writer: w, FlushInterval: 20 * time.Millisecond, NotConnectedAfter: 300 * time.Millisecond, Log: quiet()})
	stop := runCapture(t, c)
	defer stop()
	nc, err := nats.Connect("nats://127.0.0.1:1", append(c.ConnHandlers("sys"),
		nats.RetryOnFailedConnect(true), nats.MaxReconnects(-1), nats.ReconnectWait(50*time.Millisecond))...)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	waitKind(t, w, "collector.not_connected")
}

// A connection that loses its only server is recorded as disconnected,
// then as not connected.
func TestNotConnectedAfterLoss(t *testing.T) {
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	w := &flakyWriter{}
	c := New(Options{Writer: w, FlushInterval: 20 * time.Millisecond, NotConnectedAfter: 300 * time.Millisecond, Log: quiet()})
	stop := runCapture(t, c)
	defer stop()
	nc, err := nats.Connect(s.ClientURL(), append(c.ConnHandlers("sys"),
		nats.MaxReconnects(-1), nats.ReconnectWait(50*time.Millisecond))...)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	s.Shutdown()
	waitKind(t, w, "collector.disconnected")
	waitKind(t, w, "collector.not_connected")
}

// A connection that reaches its server only after failed attempts at
// start records not connected, then the connect.
func TestConnectedAfterFailedStart(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	w := &flakyWriter{}
	c := New(Options{Writer: w, FlushInterval: 20 * time.Millisecond, NotConnectedAfter: 300 * time.Millisecond, Log: quiet()})
	stop := runCapture(t, c)
	defer stop()
	nc, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", port), append(c.ConnHandlers("collector"),
		nats.RetryOnFailedConnect(true), nats.MaxReconnects(-1), nats.ReconnectWait(50*time.Millisecond))...)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	waitKind(t, w, "collector.not_connected")
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	defer s.Shutdown()
	waitKind(t, w, "collector.reconnected")
}
