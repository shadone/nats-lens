package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
)

func TestWaitUntilGivesUpAtTheDeadline(t *testing.T) {
	never := make(chan struct{})
	got := make(chan bool, 1)
	go func() { got <- waitUntil(never, time.Now().Add(100*time.Millisecond)) }()
	select {
	case ok := <-got:
		if ok {
			t.Fatal("reported done for a channel that never closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waited past the deadline")
	}
	done := make(chan struct{})
	close(done)
	if !waitUntil(done, time.Now().Add(time.Hour)) {
		t.Fatal("closed channel not reported")
	}
}

func TestStateDeadline(t *testing.T) {
	now := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if got := stateDeadline(now.Add(-time.Second), now); !got.Equal(now.Add(minStateWait)) {
		t.Errorf("passed shutdown deadline: state deadline %v, want now + %s", got, minStateWait)
	}
	if got := stateDeadline(now.Add(time.Minute), now); !got.Equal(now.Add(time.Minute)) {
		t.Errorf("distant shutdown deadline: state deadline %v, want the shutdown deadline", got)
	}
}

// The last state.json is written even when the shutdown deadline has
// already passed, as when the last events used it all up.
func TestFinalStateAfterDeadlinePassed(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t.TempDir())
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Now().UTC()
	if err := st.WriteEvents(ctx, []events.Event{{Time: at, ReceivedAt: at, Source: events.SourceCollector, Kind: "collector.started", Summary: "s"}}); err != nil {
		t.Fatal(err)
	}
	arch := newArchiver(cfg, st, archive.LocalDir{Dir: cfg.ArchiveDir}, quietLog())
	writeFinalState(ctx, arch, time.Now().Add(-time.Second), quietLog())
	b, err := os.ReadFile(filepath.Join(cfg.ArchiveDir, archive.StateRelPath))
	if err != nil {
		t.Fatalf("state.json not written: %v", err)
	}
	var state archive.State
	if err := json.Unmarshal(b, &state); err != nil || state.MaxEventID != 1 {
		t.Fatalf("state = %+v, %v", state, err)
	}
}
