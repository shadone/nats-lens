package notify

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
)

// rebuild exports st's hours up to at and rebuilds a new store from them,
// as natslens does after a lost database.
func rebuild(t *testing.T, st *store.Store, at time.Time) *store.Store {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	arch := func(s *store.Store) *archive.Archiver {
		return &archive.Archiver{Store: s, Dest: archive.LocalDir{Dir: dir}, Retention: 14 * 24 * time.Hour, Grace: time.Minute,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	}
	later := at.Truncate(time.Hour).Add(2 * time.Hour)
	if n, err := arch(st).Export(ctx, later); err != nil || n == 0 {
		t.Fatalf("export: %d hours, %v", n, err)
	}
	rb, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rb.Close() })
	if err := arch(rb).Rebuild(ctx, later); err != nil {
		t.Fatal(err)
	}
	var findings, state int
	if err := rb.DB().QueryRow(`SELECT (SELECT count(*) FROM findings), (SELECT count(*) FROM notify_state) + (SELECT count(*) FROM notify_channels) + (SELECT count(*) FROM notify_outbox)`).Scan(&findings, &state); err != nil {
		t.Fatal(err)
	}
	if findings == 0 || state != 0 {
		t.Fatalf("rebuilt store has %d findings and %d notification rows", findings, state)
	}
	return rb
}
