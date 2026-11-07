package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
)

// failingPut is a local archive whose Put fails while fail is set.
type failingPut struct {
	archive.LocalDir
	fail *bool
}

func (d failingPut) Put(ctx context.Context, rel, path string) error {
	if *d.fail {
		return errors.New("disk full")
	}
	return d.LocalDir.Put(ctx, rel, path)
}

// Before the first export nothing is listed: the hours are unknown, not 0.
func TestArchiveStatusBeforeFirstListing(t *testing.T) {
	a := newArchiver(openMem(t), t.TempDir())
	if s := a.Status(); !s.ListedAt.IsZero() || s.Hours != 0 || !s.OldestHour.IsZero() || !s.LastExportAt.IsZero() || s.LastError != "" {
		t.Errorf("status before any export = %+v", s)
	}
}

// An export lists the manifests, with or without archive_retention: the
// hours, the newest and whether it is final, and when.
func TestArchiveStatusAfterExport(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	a.ProvisionalEvery = 5 * time.Minute
	write(t, st, event(h0.Add(time.Minute), "a"), event(h1.Add(time.Minute), "b"), event(h2.Add(time.Minute), "c"))
	exported(t, a, now)
	s := a.Status()
	if !s.ListedAt.Equal(now) || s.Hours != 3 || !s.OldestHour.Equal(h0) || !s.NewestHour.Equal(h2) || s.NewestFinal ||
		!s.LastExportAt.Equal(now) {
		t.Errorf("status = %+v, want h0 to h2 listed at %v, the open hour provisional", s, now)
	}
	// The open hour closes and is exported again, final.
	later := h2.Add(time.Hour + 2*time.Minute)
	write(t, st, event(h2.Add(50*time.Minute), "d"))
	exported(t, a, later)
	if s := a.Status(); !s.NewestFinal || !s.LastExportAt.Equal(later) || s.Hours != 3 {
		t.Errorf("after the open hour closed: %+v", s)
	}
}

// After a prune the status counts only the hours left.
func TestArchiveStatusAfterPrune(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "old"), event(h1.Add(time.Minute), "young"))
	exported(t, a, now)
	if s := a.Status(); s.Hours != 2 {
		t.Fatalf("before the prune: %+v", s)
	}
	a.ArchiveRetention = 2 * time.Hour
	at := h1.Add(150 * time.Minute)
	exported(t, a, at)
	if s := a.Status(); s.Hours != 1 || !s.OldestHour.Equal(h1) || !s.NewestHour.Equal(h1) || !s.ListedAt.Equal(at) {
		t.Errorf("after the prune: %+v, want h1 alone", s)
	}
}

// After a rebuild the archive holds hours the database did not load (older
// than the retention): the status counts the archive, not archive_hours.
func TestArchiveStatusAfterRebuild(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	write(t, st, event(h0.Add(time.Minute), "old"), event(h1.Add(time.Minute), "young"))
	exported(t, newArchiver(st, dir), now)
	rb := openMem(t)
	b := newArchiver(rb, dir)
	b.Retention = 90 * time.Minute
	if err := b.Rebuild(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int64](t, rb, `SELECT count(*) FROM archive_hours`); n != 1 {
		t.Fatalf("archive_hours after the rebuild: %d hours, want h1 alone", n)
	}
	exported(t, b, now)
	if s := b.Status(); s.Hours != 2 || !s.OldestHour.Equal(h0) {
		t.Errorf("status after the rebuild: %+v, want h0 and h1 on disk", s)
	}
}

// A failed tick keeps its error and time until a tick succeeds.
func TestArchiveStatusLastError(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	fail := true
	a := newArchiver(st, dir)
	a.Dest = failingPut{LocalDir: archive.LocalDir{Dir: dir}, fail: &fail}
	write(t, st, event(h0.Add(time.Minute), "a"))
	if err := a.Tick(context.Background(), now); err == nil {
		t.Fatal("tick with a failing destination succeeded")
	}
	if s := a.Status(); s.LastError == "" || !s.LastErrorAt.Equal(now) || !s.LastExportAt.IsZero() {
		t.Errorf("after a failed tick: %+v", s)
	}
	fail = false
	if err := a.Tick(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s := a.Status(); s.LastError != "" || !s.LastErrorAt.IsZero() || s.Hours != 1 {
		t.Errorf("after a tick that succeeded: %+v", s)
	}
}
