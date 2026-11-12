package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

func sampledEpochs(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM stream_sample_epochs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The system's open fills the stream samples of an epoch stored without
// them, after the asset counts and within the store's limits. A fill that
// fails leaves the system open, with the epoch still unsampled, so its
// per-stream series read the reports, and is logged; the other system
// fills as usual.
func TestStreamSamplesFilledAtOpen(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	for _, s := range cfg.ResolvedSystems() {
		storeWithoutCounts(t, s.DataDir)
	}
	failing := cfg.ResolvedSystems()[0].ID
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, s)
	}
	origCounts, origSamples := fillAssetCounts, fillStreamSamples
	t.Cleanup(func() { fillAssetCounts, fillStreamSamples = origCounts, origSamples })
	fillAssetCounts = func(st *store.Store, ctx context.Context) (int64, error) {
		note("counts " + st.System())
		return origCounts(st, ctx)
	}
	fillStreamSamples = func(st *store.Store, ctx context.Context) (int64, error) {
		note("samples " + st.System())
		if st.System() == failing {
			return 0, errors.New("disk full")
		}
		return origSamples(st, ctx)
	}
	var logs bytes.Buffer
	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, rt := range rts {
		want := 1
		if rt.sys.ID == failing {
			want = 0
		}
		if n := sampledEpochs(t, rt.st); n != want {
			t.Errorf("%s: %d sampled epochs, want %d", rt.sys.ID, n, want)
		}
		counts, samples := -1, -1
		for i, o := range order {
			switch o {
			case "counts " + rt.sys.ID:
				counts = i
			case "samples " + rt.sys.ID:
				samples = i
			}
		}
		if counts < 0 || samples < counts {
			t.Errorf("%s: fills ran in the order %v, want the counts before the samples", rt.sys.ID, order)
		}
	}
	if !strings.Contains(logs.String(), "stream samples not filled") || !strings.Contains(logs.String(), "disk full") {
		t.Errorf("the failure was not logged: %s", logs.String())
	}
	closeSystems(rts, time.Time{})
}
