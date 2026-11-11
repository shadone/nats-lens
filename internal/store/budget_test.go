package store

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/testfatal"
	"github.com/shadone/nats-lens/internal/timing"
)

// endless is a query DuckDB needs minutes to finish, so that only a
// cancellation that reaches the engine ends it within the tests' bounds.
const endless = `SELECT sum(i) FROM range(1000000000000) t(i)`

// waitUntilTrue polls cond until it holds or 20 seconds pass.
func waitUntilTrue(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func apiCtx(ctx context.Context) context.Context {
	return admit.WithFlow(timing.WithSource(ctx, timing.SourceAPI))
}

func budgetStore(t *testing.T, system string, n int) *Store {
	t.Helper()
	s := openMem(t)
	s.SetSystem(system)
	s.SetQueryBudget(n)
	return s
}

func load(s *Store) (int, int) { return s.QueryBudget().Load() }

// runQuery runs q and reports its error, nil once its rows are read.
func runQuery(ctx context.Context, s *Store, q string) <-chan error {
	out := make(chan error, 1)
	go func() {
		var v any
		out <- s.DB().QueryRowContext(ctx, q).Scan(&v)
	}()
	return out
}

func TestQueryBudgetBoundsAPIQueriesOnly(t *testing.T) {
	s := budgetStore(t, "test-budget-bounds", 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := runQuery(apiCtx(ctx), s, endless)
	waitUntilTrue(t, "the endless query takes the slot", func() bool { a, _ := load(s); return a == 1 })
	queued := runQuery(apiCtx(context.Background()), s, `SELECT 1`)
	waitUntilTrue(t, "a second API query queues", func() bool { _, q := load(s); return q == 1 })

	// Work outside the budget runs beside the full budget.
	for _, src := range []string{timing.SourceChecks, timing.SourceEpoch, timing.SourceArchive, timing.SourceOther} {
		if err := <-runQuery(timing.WithSource(context.Background(), src), s, `SELECT 1`); err != nil {
			t.Fatalf("%s query: %v", src, err)
		}
	}
	// So does an API write, whose statements run in a transaction.
	if _, err := s.CreateMute(apiCtx(context.Background()), Mute{CheckCode: "x-001", Reason: "r", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatalf("an API write waited for the read budget: %v", err)
	}

	cancel()
	if err := <-running; !errors.Is(err, context.Canceled) {
		t.Fatalf("the canceled query ended with %v", err)
	}
	if err := <-queued; err != nil {
		t.Fatalf("the queued query: %v", err)
	}
	waitUntilTrue(t, "the budget empties", func() bool { a, q := load(s); return a == 0 && q == 0 })
}

// TestCancelReachesDuckDB cancels queries in each state they can wait in:
// executing in DuckDB, queued for the budget, and an API write waiting
// behind the write lock. Each must end soon after its cancel and leave
// nothing behind.
func TestCancelReachesDuckDB(t *testing.T) {
	const sys = "test-budget-cancel"
	s := budgetStore(t, sys, 2)
	base := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())

	var executing, queued []<-chan error
	for range 2 {
		executing = append(executing, runQuery(apiCtx(ctx), s, endless))
	}
	waitUntilTrue(t, "both slots taken", func() bool { a, _ := load(s); return a == 2 })
	for range 16 {
		queued = append(queued, runQuery(apiCtx(ctx), s, `SELECT 1`))
	}
	waitUntilTrue(t, "16 queued, the queue's bound", func() bool { _, q := load(s); return q == 16 })
	s.mu.Lock("test")
	var writes []chan error
	for range 20 {
		w := make(chan error, 1)
		writes = append(writes, w)
		go func() {
			_, err := s.CreateMute(apiCtx(ctx), Mute{CheckCode: "x-001", Reason: "r", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})
			w <- err
		}()
	}
	waitUntilTrue(t, "20 writes wait for the lock", func() bool { return timing.LockWaiters.With(sys).Load() == 20 })
	countBefore := timing.QueryDuration.Count(sys, timing.SourceAPI)
	busy := cpuOver(time.Second)
	t.Logf("CPU in the second before the cancel: %s", busy.Round(time.Millisecond))
	if busy < 900*time.Millisecond {
		t.Fatalf("the two endless queries used only %s of CPU in a second; the measure cannot tell running from stopped", busy)
	}

	canceled := time.Now()
	cancel()
	for i, ch := range append(append(executing, queued...), chanList(writes)...) {
		if err := <-ch; !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter %d ended with %v", i, err)
		}
	}
	took := time.Since(canceled)
	// The endless queries run for minutes uncanceled; a generous bound
	// still tells an interrupted engine from one left running.
	if took > 5*time.Second {
		t.Fatalf("the canceled calls took %s to end", took)
	}
	t.Logf("all 38 canceled calls ended %s after the cancel", took.Round(time.Millisecond))
	// Had the engine kept running the interrupted queries, they would use
	// most of its threads: seconds of CPU per second.
	if cpu := cpuOver(time.Second); cpu > 900*time.Millisecond {
		t.Fatalf("the process used %s of CPU in the second after the cancel; the queries still run", cpu)
	} else {
		t.Logf("CPU in the second after the cancel: %s", cpu.Round(time.Millisecond))
	}
	if a, q := load(s); a != 0 || q != 0 {
		t.Fatalf("after the cancel: %d slots taken, %d queued", a, q)
	}
	if n := timing.LockWaiters.With(sys).Load(); n != 0 {
		t.Fatalf("%d writes still wait for the lock", n)
	}
	// Only the two executing queries are recorded, when they end; the
	// queued ones never ran.
	if got := timing.QueryDuration.Count(sys, timing.SourceAPI) - countBefore; got != 2 {
		t.Fatalf("%d API queries recorded after the cancel, want the 2 interrupted", got)
	}
	waitUntilTrue(t, "goroutines back to the baseline", func() bool { return runtime.NumGoroutine() <= base+2 })
	s.mu.Unlock()
	if _, err := s.CreateMute(context.Background(), Mute{CheckCode: "x-001", Reason: "r", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatalf("a write after the canceled waiters: %v", err)
	}
}

// cpuOver returns the CPU time the process used during the next d.
func cpuOver(d time.Duration) time.Duration {
	used := func() time.Duration {
		var ru syscall.Rusage
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
		return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
	}
	start := used()
	time.Sleep(d)
	return used() - start
}

// TestEndlessQueryRunsLong is the control of TestCancelReachesDuckDB: the
// query it interrupts does not finish by itself within its bounds.
func TestEndlessQueryRunsLong(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a query for 6 s")
	}
	s := openMem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := <-runQuery(ctx, s, endless); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the endless query ended with %v before 6 s", err)
	}
}

func chanList(ws []chan error) []<-chan error {
	out := make([]<-chan error, len(ws))
	for i, w := range ws {
		out[i] = w
	}
	return out
}

func TestDeadlineInTheQueueIsOverload(t *testing.T) {
	s := budgetStore(t, "test-budget-deadline", 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := runQuery(apiCtx(ctx), s, endless)
	waitUntilTrue(t, "slot taken", func() bool { a, _ := load(s); return a == 1 })
	before := timing.AdmissionWait.Count("test-budget-deadline", timing.SourceAPI, admit.OutcomeDeadline)
	qctx, qcancel := context.WithTimeout(apiCtx(context.Background()), 100*time.Millisecond)
	defer qcancel()
	err := <-runQuery(qctx, s, `SELECT 1`)
	if !errors.Is(err, admit.ErrOverloaded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if !admit.FlowOf(qctx).Overloaded() {
		t.Fatal("flow not marked")
	}
	if got := timing.AdmissionWait.Count("test-budget-deadline", timing.SourceAPI, admit.OutcomeDeadline) - before; got != 1 {
		t.Fatalf("deadline outcomes %d", got)
	}
	cancel()
	<-running
}

func TestCloseEndsQueuedQueries(t *testing.T) {
	s, err := OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	s.SetQueryBudget(1)
	release, err := s.QueryBudget().Enter(apiCtx(context.Background()), admit.Request{Source: timing.SourceAPI})
	if err != nil {
		t.Fatal(err)
	}
	queued := runQuery(apiCtx(context.Background()), s, `SELECT 1`)
	waitUntilTrue(t, "queued", func() bool { _, q := load(s); return q == 1 })
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	if err := <-queued; !errors.Is(err, admit.ErrClosed) {
		t.Fatalf("a query queued at close got %v", err)
	}
	release()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}

func TestFatalEngineEndsQueuedQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.duckdb")
	testfatal.Prepare(t, path)
	ctx := context.Background()
	st, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetQueryBudget(1)
	release, err := st.QueryBudget().Enter(apiCtx(ctx), admit.Request{Source: timing.SourceAPI})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	queued := runQuery(apiCtx(ctx), st, `SELECT 1`)
	waitUntilTrue(t, "queued", func() bool { _, q := load(st); return q == 1 })
	tx, err := st.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM fatal_fixture"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); !IsFatal(err) {
		t.Fatalf("commit: %v", err)
	}
	if err := <-queued; !IsFatal(err) {
		t.Fatalf("a query queued at the fatal error got %v", err)
	}
}

// TestManyClientsAndCancellationsLeaveTheBudgetEmpty runs short and slow
// API queries from many goroutines, canceling a third of them, with
// /metrics queries and epoch-source queries beside them.
func TestManyClientsAndCancellationsLeaveTheBudgetEmpty(t *testing.T) {
	const sys = "test-budget-storm"
	s := budgetStore(t, sys, 3)
	var wg sync.WaitGroup
	for i := range 90 {
		wg.Go(func() {
			ctx, cancel := context.WithCancel(apiCtx(context.Background()))
			defer cancel()
			q := `SELECT count(*) FROM range(200000)`
			switch i % 3 {
			case 0:
				q = endless
				go func() { time.Sleep(time.Duration(i) * time.Millisecond); cancel() }()
			case 1:
				ctx = admit.WithFlow(timing.WithSource(ctx, timing.SourceMetrics))
			}
			err := <-runQuery(ctx, s, q)
			// The queue holds 24; a query past it is refused as overload.
			if i%3 != 0 && err != nil && !errors.Is(err, admit.ErrOverloaded) {
				t.Errorf("query %d: %v", i, err)
			}
		})
	}
	for range 10 {
		wg.Go(func() {
			if err := <-runQuery(timing.WithSource(context.Background(), timing.SourceEpoch), s, `SELECT count(*) FROM range(200000)`); err != nil {
				t.Errorf("epoch-source query: %v", err)
			}
		})
	}
	wg.Wait()
	if a, q := load(s); a != 0 || q != 0 {
		t.Fatalf("after the storm: %d slots taken, %d queued", a, q)
	}
	if timing.AdmissionActive.With(sys).Load() != 0 || timing.AdmissionQueued.With(sys).Load() != 0 {
		t.Fatal("gauges not back to 0")
	}
}

// While the epoch write holds the store's lock, the budget admits one
// API query; other writes leave it alone.
func TestEpochWriteYieldsTheBudget(t *testing.T) {
	s := budgetStore(t, "test-budget-yield", 4)
	g := s.QueryBudget()
	s.mu.Lock(opMutes)
	if g.Limit() != 4 || !g.Idle() {
		t.Fatal("a mute write narrowed the budget")
	}
	s.mu.Unlock()
	s.mu.Lock(opEpoch)
	one, err := g.Enter(apiCtx(context.Background()), admit.Request{Source: timing.SourceAPI})
	if err != nil {
		t.Fatal(err)
	}
	if g.Idle() {
		t.Fatal("the budget admits a second query during the epoch write")
	}
	s.mu.Unlock()
	if !g.Idle() {
		t.Fatal("the budget stayed narrow after the epoch write")
	}
	one()
}

// A read transaction of the API takes one slot when it begins and gives it
// back when it ends, by commit or rollback; its statements take none.
func TestReadTransactionHoldsOneSlot(t *testing.T) {
	s := budgetStore(t, "test-budget-readtx", 1)
	for _, end := range []string{"commit", "rollback"} {
		tx, err := s.DB().BeginTx(ReadTx(apiCtx(context.Background())), nil)
		if err != nil {
			t.Fatal(err)
		}
		if a, _ := load(s); a != 1 {
			t.Fatalf("%s: %d slots taken by an open read transaction", end, a)
		}
		var n int
		if err := tx.QueryRowContext(context.Background(), `SELECT 1`).Scan(&n); err != nil {
			t.Fatalf("%s: a statement in the read transaction: %v", end, err)
		}
		queued := runQuery(apiCtx(context.Background()), s, `SELECT 1`)
		waitUntilTrue(t, "a query outside waits", func() bool { _, q := load(s); return q == 1 })
		if end == "commit" {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := <-queued; err != nil {
			t.Fatal(err)
		}
		waitUntilTrue(t, "budget empty", func() bool { a, q := load(s); return a == 0 && q == 0 })
	}
}
