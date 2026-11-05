package checks

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
)

// every5m stores epochs 1 to n five minutes apart from t0 (09:00 UTC):
// epoch 13 is 10:00, 37 is 12:00 and 38 is 12:05.
func (f *fx) every5m(n int64) {
	f.t.Helper()
	for e := int64(1); e <= n; e++ {
		f.epochAt(e, t0.Add(time.Duration(e-1)*5*time.Minute), []string{"nats-0"})
	}
}

// exported records hour (an hour after t0) as exported at at.
func (f *fx) exported(hour int, at time.Time, final bool) {
	f.t.Helper()
	f.row("archive_hours", m{"hour": t0.Add(time.Duration(hour) * time.Hour), "rows": 10, "files": 1,
		"exported_at": at, "final": final})
}

// The hour 10:00 ended at 11:00 and is not exported: it has waited the
// default max_delay of an hour at 12:00, which does not fire, and fires
// at 12:05. Every closed hour exported fires nothing.
func TestArchiveExportBehind(t *testing.T) {
	f := newFx(t)
	f.archiveSince = t0.Add(-24 * time.Hour)
	f.every5m(38)
	f.exported(0, t0.Add(61*time.Minute), true)
	f.exported(2, t0.Add(150*time.Minute), false)
	f.fires("system-008", 37)
	r := f.fires("system-008", 38, "")
	want(t, r[""], "warning", "The hour 2026-10-17 10:00 UTC ended 1.1 hours ago and has not been exported to the archive since, nor have 1 later closed hour. The last export was at 2026-10-17 11:30 UTC.")
	if v := r[""].Value; v == nil || *v != 1.1 {
		t.Errorf("value %v, want 1.1", v)
	}

	g := newFx(t)
	g.archiveSince = t0.Add(-24 * time.Hour)
	g.every5m(38)
	for h := range 3 {
		g.exported(h, t0.Add(time.Duration(h+1)*time.Hour+time.Minute), true)
	}
	g.fires("system-008", 38)
}

// natslens once does not export: without archive_since nothing fires.
func TestArchiveExportBehindArchiveOff(t *testing.T) {
	f := newFx(t)
	f.every5m(38)
	f.fires("system-008", 38)
}

// After a restart the hours waiting get max_delay from the start, so the
// catch-up does not fire; one that has not caught up an hour after the
// start does.
func TestArchiveExportBehindAfterRestart(t *testing.T) {
	f := newFx(t)
	f.archiveSince = t0.Add(3 * time.Hour)
	f.every5m(50)
	f.fires("system-008", 38)
	f.fires("system-008", 49)
	r := f.fires("system-008", 50, "")
	want(t, r[""], "warning", "The hour 2026-10-17 09:00 UTC ended 3.1 hours ago", "nor have 3 later closed hours", "No hour has been exported yet.")
	if v := r[""].Value; v == nil || *v != 1.1 {
		t.Errorf("value %v, want 1.1 (hours since the start)", v)
	}
}

// An hour that holds only events waits for export too; an open hour does
// not count.
func TestArchiveExportBehindEventsOnly(t *testing.T) {
	f := newFx(t)
	f.archiveSince = t0.Add(-24 * time.Hour)
	f.epochAt(1, t0.Add(-time.Hour), []string{"nats-0"})
	f.exported(-1, t0.Add(time.Minute), true)
	f.event(1, t0.Add(10*time.Minute), "collector.not_connected", m{"source": "collector"})
	f.epochAt(2, t0.Add(2*time.Hour+5*time.Minute), []string{"nats-0"})
	r := f.fires("system-008", 2, "")
	want(t, r[""], "warning", "The hour 2026-10-17 09:00 UTC ended 1.1 hours ago and has not been exported to the archive since. The last export was at 2026-10-17 09:01 UTC.")
	f.epochAt(3, t0.Add(2*time.Hour+30*time.Minute), []string{"nats-0"})
	f.exported(0, t0.Add(2*time.Hour+time.Minute), true)
	f.fires("system-008", 3)
}

// The hours the archiver itself exports read as exported, also in a
// session time zone other than UTC.
func TestArchiveExportBehindAfterRealExport(t *testing.T) {
	f := newFx(t)
	f.archiveSince = t0.Add(-24 * time.Hour)
	f.every5m(38)
	a := &archive.Archiver{Store: f.st, Dest: archive.LocalDir{Dir: t.TempDir()}, Retention: 14 * 24 * time.Hour,
		Grace: time.Minute, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	n, err := a.Export(context.Background(), t0.Add(3*time.Hour+6*time.Minute))
	if err != nil || n != 3 {
		t.Fatalf("exported %d hours, %v; want 3", n, err)
	}
	f.fires("system-008", 38)
}
