package perf

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/timing"
)

// One Overview load at a query budget of one slot, the budget of a host
// with two CPUs, is never refused: with the slot held, all of the page's
// first queries wait in the queue at once, and every call answers once
// the slot frees.
func TestOneOverviewLoadFitsTheQueueOfOneSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("generates a store: run without -short")
	}
	ctx := context.Background()
	p := Profiles["smoke"]
	path := filepath.Join(t.TempDir(), "smoke.duckdb")
	if _, err := Generate(ctx, p, path, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	h, closeAll, err := openHarness(ctx, LoadOptions{Profile: p, Path: path, QueryBudget: 1, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	g := h.st.QueryBudget()
	hold, err := g.Enter(ctx, admit.Request{Source: timing.SourceAPI})
	if err != nil {
		t.Fatal(err)
	}
	page := overviewPage(h.now, day)
	rec := &recorder{}
	done := make(chan struct{})
	go func() { h.loadPage(ctx, rec, page); close(done) }()
	// Every call of the page waits for its first query.
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, q := g.Load(); q >= len(page.calls) {
			break
		}
		rec.mu.Lock()
		answered := len(rec.samples)
		rec.mu.Unlock()
		if time.Now().After(deadline) || answered > 0 {
			_, q := g.Load()
			hold()
			<-done
			t.Fatalf("%d of the page's %d calls queued; answered early: %+v", q, len(page.calls), rec.samples)
		}
	}
	t.Logf("%d queries of one Overview load waited for the one slot", g.TakePeak())
	hold()
	<-done
	for _, s := range rec.samples {
		if s.err != nil {
			t.Errorf("%s: %v", s.proc, s.err)
		}
	}
}
