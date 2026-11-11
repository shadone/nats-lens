package main

import (
	"context"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/timing"
)

// openStore labels every query of the open with the system's ID: the
// emptiness check, the rebuild into a side file, and the open of an
// existing file.
func TestOpenStoreLabelsItsQueries(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t.TempDir())
	cfg.SystemID = "open-store-test"
	writeArchive(t, cfg)
	empty, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	empty.Close()

	unlabelled := timing.QueryDuration.Count("", timing.SourceOther)
	// The file is empty, so this open rebuilds it from the archive.
	st, _, err := openStore(ctx, cfg, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if n := countEvents(t, st); n != 2 {
		t.Fatalf("events after rebuild = %d, want 2", n)
	}
	st.Close()
	// Now the file holds data: the open checks it and keeps it.
	st, _, err = openStore(ctx, cfg, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	if got := timing.QueryDuration.Count("", timing.SourceOther) - unlabelled; got != 0 {
		t.Errorf("openStore recorded %d queries without a system", got)
	}
	if timing.QueryDuration.Count(cfg.SystemID, timing.SourceOther) == 0 {
		t.Errorf("openStore recorded no query under system=%q", cfg.SystemID)
	}
}
