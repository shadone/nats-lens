package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// flakyWriter fails its first failFor calls, or every call when failFor is
// negative.
type flakyWriter struct {
	mu      sync.Mutex
	calls   atomic.Int64
	failFor int64
	evs     []Event
}

func (w *flakyWriter) WriteEvents(_ context.Context, evs []Event) error {
	n := w.calls.Add(1)
	if w.failFor < 0 || n <= w.failFor {
		return errors.New("database unavailable")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evs = append(w.evs, evs...)
	return nil
}

func (w *flakyWriter) written() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.evs)
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func runCapture(t *testing.T, c *Capture) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

func record(c *Capture, n int) {
	for range n {
		c.Record(Event{Kind: "x", Summary: "x", Time: recv, ReceivedAt: recv})
	}
}

// While writes fail, a full batch does not trigger a write per message:
// retries wait for the ticker and back off.
func TestFailingWriterBacksOff(t *testing.T) {
	w := &flakyWriter{failFor: -1}
	c := New(Options{Writer: w, BatchSize: 10, FlushInterval: 50 * time.Millisecond, Log: quiet()})
	stop := runCapture(t, c)
	record(c, 2000)
	time.Sleep(600 * time.Millisecond)
	stop()
	// One attempt for the first full batch, then at most one per backoff
	// step (50, 100, 200, 400 ms), and one at shutdown.
	if n := w.calls.Load(); n > 8 {
		t.Fatalf("%d write attempts in 600ms while failing", n)
	}
}

func TestWritesResumeAfterFailures(t *testing.T) {
	w := &flakyWriter{failFor: 3}
	c := New(Options{Writer: w, BatchSize: 10, FlushInterval: 20 * time.Millisecond, Log: quiet()})
	stop := runCapture(t, c)
	record(c, 100)
	deadline := time.Now().Add(3 * time.Second)
	for w.written() < 100 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	if got := w.written(); got != 100 {
		t.Fatalf("written %d of 100 after the writer recovered", got)
	}
}

// Everything received before shutdown is written.
func TestShutdownWritesEverything(t *testing.T) {
	w := &flakyWriter{}
	c := New(Options{Writer: w, FlushInterval: time.Hour, Log: quiet()})
	stop := runCapture(t, c)
	record(c, 50)
	stop()
	if got := w.written(); got != 50 {
		t.Fatalf("wrote %d of 50", got)
	}
}

// Held actions expire by the receive time of the last processed message
// while messages are queued, so an audit still in the queue can match.
func TestExpireClock(t *testing.T) {
	now, last := recv.Add(time.Minute), recv
	if got := expireClock(3, last, now); !got.Equal(last) {
		t.Errorf("with a backlog the clock is the last processed message: %v", got)
	}
	if got := expireClock(0, last, now); !got.Equal(now) {
		t.Errorf("with an empty queue the clock is now: %v", got)
	}
	if got := expireClock(3, time.Time{}, now); !got.Equal(now) {
		t.Errorf("before any message the clock is now: %v", got)
	}
}

// The server-wide and the account-scoped event of one failed login have
// different payload IDs; only one is stored.
func TestAuthErrorStoredOnce(t *testing.T) {
	r := loadFixture(t, "auth_error")
	c := New(Options{Writer: &flakyWriter{}, Log: quiet()})
	cor, seen := newCorrelator(), map[string]time.Time{}
	first := c.handle(cor, seen, incoming{sys: true, subject: r.Subject, data: r.Payload, at: recv})
	scoped := []byte(strings.Replace(string(r.Payload), `"id": "`, `"id": "other`, 1))
	second := c.handle(cor, seen, incoming{account: "APP", subject: authErrAccountSubject, data: scoped, at: recv})
	if len(first) != 1 || first[0].Kind != "client.auth_error" || len(second) != 0 {
		t.Fatalf("first = %d events, second = %d", len(first), len(second))
	}
}

// Account connection counts are captured only when asked for.
func TestAccountConnectionsOptIn(t *testing.T) {
	if slices.Contains(SysSubjects(false), "$SYS.ACCOUNT.*.SERVER.CONNS") {
		t.Error("account connection counts subscribed by default")
	}
	if !slices.Contains(SysSubjects(true), "$SYS.ACCOUNT.*.SERVER.CONNS") {
		t.Error("account connection counts not subscribed when enabled")
	}
}
