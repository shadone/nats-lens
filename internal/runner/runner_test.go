package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

func newRunner(t *testing.T) *Runner {
	t.Helper()
	c := testcluster.Start(t, 1)
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	col := collect.New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), collect.Options{
		Window: 100 * time.Millisecond, ConnzLimit: 10, Tracker: collect.NewTracker(time.Hour),
	})
	// Log is left nil on purpose: the runner must fall back to slog.Default.
	return &Runner{Collector: col, Store: st, Interval: 300 * time.Millisecond, Retention: time.Hour}
}

func epochs(t *testing.T, r *Runner) int {
	t.Helper()
	var n int
	if err := r.Store.DB().QueryRow(`SELECT count(*) FROM epochs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRunTakesEpochsUntilCancelled(t *testing.T) {
	r := newRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	if n := epochs(t, r); n < 2 {
		t.Fatalf("epochs = %d, want at least 2", n)
	}
	var partial bool
	if err := r.Store.DB().QueryRow(`SELECT partial FROM epochs ORDER BY epoch LIMIT 1`).Scan(&partial); err != nil || partial {
		t.Fatalf("first epoch partial=%v err=%v", partial, err)
	}
}

func TestOnceWithCancelledContextStoresNothing(t *testing.T) {
	r := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Once(ctx); err == nil {
		t.Fatal("expected the context error")
	}
	if n := epochs(t, r); n != 0 {
		t.Fatalf("epochs = %d", n)
	}
}

func TestWriteContextOutlivesShutdownByGraceOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	wctx, done := writeContext(ctx, 150*time.Millisecond)
	defer done()
	cancel()
	select {
	case <-wctx.Done():
		t.Fatal("write context ended with the parent; it must get the grace period")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-wctx.Done():
	case <-time.After(time.Second):
		t.Fatal("write context outlived the grace period")
	}
}

// Every stored epoch is checked: each check has a run for it.
func TestOnceRunsChecks(t *testing.T) {
	r := newRunner(t)
	set, err := checks.Load(config.Checks{Timeout: config.Duration(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	r.Checks = &checks.Engine{Set: set}
	epoch, err := r.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var n, failed int
	if err := r.Store.DB().QueryRow(`SELECT count(*), count(*) FILTER (WHERE status <> 'ok') FROM check_runs WHERE epoch = ?`, epoch).
		Scan(&n, &failed); err != nil {
		t.Fatal(err)
	}
	if n != len(set.Checks()) || failed != 0 {
		t.Fatalf("check runs for epoch %d: %d (%d not ok), want %d", epoch, n, failed, len(set.Checks()))
	}
}

func TestOnceCallsStored(t *testing.T) {
	r := newRunner(t)
	var got []int64
	r.Stored = func(epoch int64) { got = append(got, epoch) }
	epoch, err := r.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != epoch {
		t.Fatalf("stored called with %v, want [%d]", got, epoch)
	}
}

func TestRunSkipsEpochsWhileNotConnected(t *testing.T) {
	r := newRunner(t)
	var connected atomic.Bool
	r.Connected = connected.Load
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	time.Sleep(700 * time.Millisecond)
	if n := epochs(t, r); n != 0 {
		t.Fatalf("%d epochs while not connected", n)
	}
	connected.Store(true)
	time.Sleep(700 * time.Millisecond)
	cancel()
	<-done
	if n := epochs(t, r); n == 0 {
		t.Fatal("no epoch once connected")
	}
}
