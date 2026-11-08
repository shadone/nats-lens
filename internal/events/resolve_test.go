package events

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// A user that may not request $SYS.REQ.USER.INFO keeps its label; the
// denial is stored as no event, and neither a second connection nor a
// redial of the same account asks again. Adapted from the review's test.
func TestUserInfoDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		Users: []*server.User{{Username: "u", Password: "p",
			Permissions: &server.Permissions{Publish: &server.SubjectPermission{Allow: []string{"foo.>"}},
				Subscribe: &server.SubjectPermission{Allow: []string{">"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	defer s.Shutdown()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	w := &flakyWriter{}
	c := New(Options{Writer: w, FlushInterval: 50 * time.Millisecond, Log: quiet()})
	stop := runCapture(t, c)
	connect := func() *nats.Conn {
		nc, err := nats.Connect(s.ClientURL(), append(c.ConnHandlers("acc"), nats.UserInfo("u", "p"))...)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.SubscribeAccount("acc", nc); err != nil {
			t.Fatal(err)
		}
		return nc
	}
	first := connect()
	deadline := time.Now().Add(5 * time.Second)
	for !c.resolveDenied("acc") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !c.resolveDenied("acc") {
		t.Fatal("denial not noticed")
	}
	first.Close()
	second := connect() // a redial
	time.Sleep(3 * time.Second)
	second.Close()
	stop()
	for _, k := range kinds(w) {
		if k == "collector.subscription_error" || k == "collector.connection_closed" {
			t.Fatalf("denied account name lookup stored as an event: %v", kinds(w))
		}
	}
	if n := c.resolveRequests("acc"); n != 1 {
		t.Fatalf("account name requested %d times, want once per process", n)
	}
}

// syncBuffer is a log sink safe for concurrent writes.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Every connection whose account name stays unknown says so, also one
// that starts after the requests for its label are used up.
func TestResolveBudgetSpentIsLogged(t *testing.T) {
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true, NoSystemAccount: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	defer s.Shutdown()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	var logs syncBuffer
	c := New(Options{Writer: &flakyWriter{}, Log: slog.New(slog.NewTextHandler(&logs, nil))})
	c.resolveWait = 10 * time.Millisecond
	warnings := func() int { return strings.Count(logs.String(), "events use the configured label") }
	for i := range 2 {
		nc, err := nats.Connect(s.ClientURL())
		if err != nil {
			t.Fatal(err)
		}
		if err := c.SubscribeAccount("acc", nc); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for warnings() < i+1 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		nc.Close()
	}
	if n := warnings(); n != 2 {
		t.Fatalf("warnings = %d, want one per connection:\n%s", n, logs.String())
	}
	if n := c.resolveRequests("acc"); n != resolveAttempts {
		t.Fatalf("requests = %d, want %d", n, resolveAttempts)
	}
}

// A user that may request $SYS.REQ.USER.INFO but may not subscribe to its
// reply inbox is refused the lookup as well: it keeps its label, asks once,
// and the inbox denial is stored as no event.
func TestUserInfoInboxDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		Users: []*server.User{{Username: "u", Password: "p",
			Permissions: &server.Permissions{Publish: &server.SubjectPermission{Allow: []string{"$SYS.REQ.USER.INFO"}},
				Subscribe: &server.SubjectPermission{Allow: []string{"$JS.EVENT.>", "$SYS.ACCOUNT.CLIENT.AUTH.ERR"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	defer s.Shutdown()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	w := &flakyWriter{}
	c := New(Options{Writer: w, FlushInterval: 50 * time.Millisecond, Log: quiet()})
	c.resolveWait = 200 * time.Millisecond
	stop := runCapture(t, c)
	nc, err := nats.Connect(s.ClientURL(), append(c.ConnHandlers("acc"), nats.UserInfo("u", "p"))...)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SubscribeAccount("acc", nc); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !c.resolveDenied("acc") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !c.resolveDenied("acc") {
		t.Fatalf("inbox denial not taken as a refused lookup; requests %d", c.resolveRequests("acc"))
	}
	time.Sleep(500 * time.Millisecond)
	nc.Close()
	stop()
	for _, k := range kinds(w) {
		if k == "collector.subscription_error" {
			t.Fatalf("inbox denial stored as an event: %v", kinds(w))
		}
	}
	if n := c.resolveRequests("acc"); n != 1 {
		t.Fatalf("account name requested %d times, want once", n)
	}
}
