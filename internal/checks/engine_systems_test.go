package checks

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// clientRows runs client-001 for the epoch with e and returns its rows.
func clientRows(t *testing.T, e *Engine, f *fx, epoch int64) map[string]store.FindingRow {
	t.Helper()
	e.only = map[string]bool{"client-001": true}
	runs, err := e.Evaluate(context.Background(), f.st.DB(), epoch)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.FindingRow{}
	for _, r := range runs {
		for _, row := range r.Rows {
			out[row.EntityKey] = row
		}
	}
	return out
}

// Engines of different systems share one Set but each leaves out its own
// system's nats-lens connections only.
func TestEngineToolingNamesAreTheEngines(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	for _, e := range []int64{99, 100} {
		f.row("connections", m{"epoch": e, "server_id": "S0", "cid": 1, "name": "natslens-shop-prod", "rtt_ns": 300e6, "ip": "10.0.0.1"})
		f.row("connections", m{"epoch": e, "server_id": "S0", "cid": 2, "name": "natslens-shop-staging", "rtt_ns": 300e6, "ip": "10.0.0.2"})
	}
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	set.SetToolingNames([]string{"natslens-shop-staging"})
	prod := &Engine{Set: set, ToolingNames: []string{"natslens-shop-prod"}}
	if got := clientRows(t, prod, f, 100); len(got) != 1 {
		t.Errorf("prod engine: %s, want only the staging connection", rowsText(got))
	}
	shared := &Engine{Set: set}
	if got := clientRows(t, shared, f, 100); len(got) != 1 {
		t.Errorf("engine without names: %s, want the Set's names left out", rowsText(got))
	}
}

// Engines sharing a Set run at the same time without a data race, and
// each logs the loopback guard once for its own system. Run with -race.
func TestEnginesShareASetConcurrently(t *testing.T) {
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		f := newFx(t)
		f.cluster(100)
		for cid, ip := range []string{"127.0.0.1", "127.0.0.1", "::1", "127.0.0.1", "10.0.0.9"} {
			f.row("connections", m{"epoch": 100, "server_id": "S0", "cid": cid + 1, "name": "svc", "rtt_ns": 1e6, "ip": ip})
		}
		e := &Engine{Set: set, Log: slog.New(slog.DiscardHandler)}
		wg.Go(func() {
			if _, err := e.Evaluate(context.Background(), f.st.DB(), 100); err != nil {
				t.Error(err)
			}
			if !e.loopbackWarned {
				t.Error("the loopback guard was not logged by this engine")
			}
		})
	}
	wg.Wait()
}
