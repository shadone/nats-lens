package storeprov

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/store"
)

func TestCapturedProviderConstructionBoundary(t *testing.T) {
	ctx := context.Background()
	reference := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	fingerprint := strings.Repeat("a", 64)
	if _, err := NewCaptured(ctx, nil, 0, reference, fingerprint); err == nil {
		t.Fatal("missing cadence accepted")
	}
	if _, err := NewCaptured(ctx, nil, time.Second, time.Time{}, fingerprint); err == nil {
		t.Fatal("wall clock fallback accepted")
	}
	if _, err := NewCaptured(ctx, nil, time.Second, reference, fingerprint); err == nil {
		t.Fatal("missing readonly DB accepted")
	}
	path := filepath.Join(t.TempDir(), "closed.duckdb")
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewCaptured(ctx, st.DB(), time.Second, reference, fingerprint); err == nil {
		t.Fatal("writable DB accepted")
	}
	st.Close()
	db, err := dataset.OpenDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = NewCaptured(ctx, db, time.Second, reference, "bad"); err == nil {
		t.Fatal("unbound immutable pagination accepted")
	}
	p, err := NewCaptured(ctx, db, time.Second, reference, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	info, err := p.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Now().Equal(reference) || !p.RangeReference().Equal(reference) || !info.NowFixed || info.Retention != nil || p.Live || p.ArchiveStatus != nil || p.Checks != nil {
		t.Fatalf("runtime/configuration invented: %+v", info)
	}
	p.St.Close()
	if err = db.PingContext(ctx); err != nil {
		t.Fatalf("borrowed DB closed: %v", err)
	}
}
