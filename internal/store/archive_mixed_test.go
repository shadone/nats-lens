package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
)

// stripAcceptanceColumns rewrites the mutes files of hour h as a version
// before 0031 wrote them: without accepted, growth_per_day, end_reason.
func stripAcceptanceColumns(t *testing.T, st *store.Store, dir string, h time.Time) int {
	t.Helper()
	files, err := (archive.LocalDir{Dir: dir}).List(context.Background(), "mutes/")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range files {
		if !strings.Contains(f, h.Format("2006-01-02")) || !strings.Contains(f, h.Format("15")) {
			continue
		}
		file := filepath.Join(dir, f)
		old := file + ".old.parquet"
		q := fmt.Sprintf(`COPY (SELECT * EXCLUDE (accepted, growth_per_day, end_reason)
  FROM read_parquet('%s', hive_partitioning = false)) TO '%s' (FORMAT parquet)`, file, old)
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(old, file); err != nil {
			t.Fatal(err)
		}
		n++
	}
	return n
}

// Mixed archive: h0's mutes file is from before 0031, h1 and h2 carry the
// baselined acceptance; optionally h2 carries it lifted. The rebuild keeps
// one row, with the growth, and the lifted copy when there is one.
func TestRebuildMixesMutesFilesFromBeforeAndAfterAcceptances(t *testing.T) {
	ctx := context.Background()
	for _, lift := range []bool{false, true} {
		for threads := 1; threads <= 4; threads += 3 {
			t.Run(fmt.Sprintf("lift=%v/threads=%d", lift, threads), func(t *testing.T) {
				st, dir := openMem(t), t.TempDir()
				a := newArchiver(st, dir)
				write(t, st, event(h0.Add(time.Minute), "a"))
				m, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/orders",
					Reason: "r", MutedBy: "ops@example.com", CreatedAt: h0, ExpiresAt: h0.Add(90 * 24 * time.Hour), Accepted: true})
				if err != nil {
					t.Fatal(err)
				}
				if n := exported(t, a, h1.Add(30*time.Minute)); n < 1 {
					t.Fatalf("first export = %d", n)
				}
				if ok, err := st.SetAcceptanceBaseline(ctx, m.ID, h1, 10<<20); err != nil || !ok {
					t.Fatalf("baseline %v %v", ok, err)
				}
				write(t, st, event(h1.Add(time.Minute), "b"))
				if n := exported(t, a, h2.Add(time.Minute)); n < 1 {
					t.Fatalf("second export = %d", n)
				}
				if lift {
					if ok, err := st.EndAcceptance(ctx, m.ID, h2, "nats-lens", "growth changed"); err != nil || !ok {
						t.Fatalf("end %v %v", ok, err)
					}
				}
				write(t, st, event(h2.Add(time.Minute), "c"))
				if n := exported(t, a, now.Add(time.Hour)); n < 1 {
					t.Fatalf("third export = %d", n)
				}
				if n := stripAcceptanceColumns(t, st, dir, h0); n == 0 {
					t.Fatal("no h0 mutes file to strip")
				}
				rb := openMem(t)
				if err := rb.SetThreads(ctx, threads); err != nil {
					t.Fatal(err)
				}
				if err := newArchiver(rb, dir).Rebuild(ctx, now.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				all, _, err := rb.ListMutes(ctx, now, true, 10, 0)
				if err != nil || len(all) != 1 {
					t.Fatalf("mutes %+v %v, want one", all, err)
				}
				got := all[0]
				if got.GrowthPerDay == nil || *got.GrowthPerDay != 10<<20 || !got.Accepted {
					t.Errorf("rebuilt %+v, want accepted with growth", got)
				}
				if lift && (got.UnmutedBy != "nats-lens" || got.EndReason == "") {
					t.Errorf("rebuilt %+v, want the ended copy", got)
				}
				if !lift && got.UnmutedBy != "" {
					t.Errorf("rebuilt %+v, want in force", got)
				}
			})
		}
	}
}

// From mutes files that are all older than the acceptance columns, the
// branch that adds growth_per_day for the order adds no row.
func TestRebuildFromOldMutesFilesOnlyAddsNoRow(t *testing.T) {
	dir, _, _, _ := archiveMutes(t)
	st := openMem(t)
	stripAcceptanceColumns(t, st, dir, h0)
	rb := rebuilt(t, dir, now)
	var n, nullID int
	if err := rb.DB().QueryRow(`SELECT count(*), count(*) FILTER (WHERE id IS NULL) FROM mutes`).Scan(&n, &nullID); err != nil {
		t.Fatal(err)
	}
	if n != 3 || nullID != 0 {
		t.Errorf("mutes after rebuild = %d (%d without id), want 3", n, nullID)
	}
}

// A baselined acceptance kept for many hours: each later hour is exported
// once, not again on every tick.
func TestBaselineIsNotExportedAgainOnEveryTick(t *testing.T) {
	ctx := context.Background()
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	g := float64(10 << 20)
	if _, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/orders",
		Reason: "r", MutedBy: "ops@example.com", CreatedAt: h0, ExpiresAt: h0.Add(90 * 24 * time.Hour), Accepted: true, GrowthPerDay: &g}); err != nil {
		t.Fatal(err)
	}
	write(t, st, event(h0.Add(time.Minute), "a"))
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("first export = %d", n)
	}
	for i := 1; i <= 5; i++ {
		if n := exported(t, a, now.Add(time.Duration(i)*time.Minute)); n != 0 {
			t.Fatalf("tick %d exported %d hours with no change", i, n)
		}
	}
}
