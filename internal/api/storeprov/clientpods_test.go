package storeprov

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// The client pod names of a range are read once per collect interval and
// kept, not scanned on every lane request.
func TestClientPodNamesCached(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for e, name := range map[int64]string{1: "svc@pod-old", 2: "svc@pod-new"} {
		at := t0.Add(time.Duration(e) * 30 * time.Second)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [], NULL)`, e, at, at); err != nil {
			t.Fatal(err)
		}
		if _, err := st.DB().Exec(`INSERT INTO connections (epoch, server_id, cid, name) VALUES (?, 'S0', 1, ?)`, e, name); err != nil {
			t.Fatal(err)
		}
	}
	p := &Provider{St: st, Interval: 30 * time.Second}
	start, end := t0, t0.Add(time.Hour)
	names, err := p.clientPodNames(ctx, start, end)
	if err != nil || len(names) != 2 {
		t.Fatalf("names %v %v", names, err)
	}
	if _, err := p.clientPodNames(ctx, start, end.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if p.podCache.reads != 1 {
		t.Errorf("read %d times for two requests within one interval, want once", p.podCache.reads)
	}
	// Two ranges asked in turn (the overview and a detail page) each keep
	// their entry.
	other := start.Add(-6 * time.Hour)
	for range 3 {
		if _, err := p.clientPodNames(ctx, other, end); err != nil {
			t.Fatal(err)
		}
		if _, err := p.clientPodNames(ctx, start, end); err != nil {
			t.Fatal(err)
		}
	}
	if p.podCache.reads != 2 {
		t.Errorf("read %d times for two ranges in turn, want twice", p.podCache.reads)
	}
	// The cache holds at most podCacheSize ranges.
	for i := range 20 {
		if _, err := p.clientPodNames(ctx, start.Add(-time.Duration(i+10)*time.Hour), end); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(p.podCache.entries); n > podCacheSize {
		t.Errorf("cache holds %d ranges, want at most %d", n, podCacheSize)
	}
}
