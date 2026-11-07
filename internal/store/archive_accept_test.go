package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
)

// archiveMutes stores an event in h0, a plain mute and two acceptances, one
// of them ended by nats-lens, exports, and returns the archive directory
// and the IDs of the mute, the acceptance in force and the ended one.
func archiveMutes(t *testing.T) (dir string, plain, inForce, ended int64) {
	t.Helper()
	ctx := context.Background()
	st := openMem(t)
	dir = t.TempDir()
	write(t, st, event(h0.Add(time.Minute), "a"))
	create := func(m store.Mute) int64 {
		t.Helper()
		m.Reason, m.MutedBy, m.CreatedAt, m.ExpiresAt = "r", "ops@example.com", h0, h0.Add(90*24*time.Hour)
		out, err := st.CreateMute(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	g := 2.5 * (1 << 20)
	plain = create(store.Mute{CheckCode: "stream-025"})
	inForce = create(store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/order-archive", Accepted: true, GrowthPerDay: &g})
	ended = create(store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/feature-flags", Accepted: true, GrowthPerDay: &g})
	if ok, err := st.EndAcceptance(ctx, ended, h1, "nats-lens", "growth changed from +2.5 MiB/day to +6.0 MiB/day"); err != nil || !ok {
		t.Fatalf("end acceptance: %v %v", ok, err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d hours", n)
	}
	return dir, plain, inForce, ended
}

func rebuiltMutes(t *testing.T, dir string) map[int64]store.Mute {
	t.Helper()
	rb := rebuilt(t, dir, now)
	all, _, err := rb.ListMutes(context.Background(), now, true, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]store.Mute{}
	for _, m := range all {
		out[m.ID] = m
	}
	return out
}

// The archive carries acceptances, their growth and end reason, through
// export and rebuild.
func TestArchiveKeepsAcceptances(t *testing.T) {
	dir, plain, inForce, ended := archiveMutes(t)
	ms := rebuiltMutes(t, dir)
	if m := ms[plain]; m.Accepted || m.GrowthPerDay != nil || m.EndReason != "" {
		t.Errorf("plain mute after rebuild = %+v", m)
	}
	if m := ms[inForce]; !m.Accepted || m.GrowthPerDay == nil || *m.GrowthPerDay != 2.5*(1<<20) || !m.Active(now) {
		t.Errorf("acceptance in force after rebuild = %+v", m)
	}
	if m := ms[ended]; !m.Accepted || m.UnmutedBy != "nats-lens" || m.EndReason != "growth changed from +2.5 MiB/day to +6.0 MiB/day" || m.Active(now) {
		t.Errorf("ended acceptance after rebuild = %+v", m)
	}
}

// A mutes file written before migration 0031 has no acceptance columns;
// the rebuild loads its rows as plain mutes: accepted false, no growth, no
// end reason.
func TestRebuildBeforeAcceptancesLoadsPlainMutes(t *testing.T) {
	dir, plain, inForce, _ := archiveMutes(t)
	files, err := (archive.LocalDir{Dir: dir}).List(context.Background(), "mutes/")
	if err != nil || len(files) == 0 {
		t.Fatalf("mutes files %v, err %v", files, err)
	}
	st := openMem(t)
	for _, f := range files {
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
		var cols int
		if err := st.DB().QueryRow(fmt.Sprintf(`SELECT count(*) FROM parquet_schema('%s') WHERE name IN ('accepted', 'growth_per_day', 'end_reason')`, file)).Scan(&cols); err != nil || cols != 0 {
			t.Fatalf("rewritten file still has %d acceptance columns, %v", cols, err)
		}
	}
	ms := rebuiltMutes(t, dir)
	if len(ms) != 3 {
		t.Fatalf("mutes after rebuild = %d, want 3", len(ms))
	}
	for id, m := range ms {
		if m.Accepted || m.GrowthPerDay != nil || m.EndReason != "" {
			t.Errorf("mute %d from an old file = %+v, want a plain mute", id, m)
		}
	}
	if !ms[plain].Active(now) || !ms[inForce].Active(now) {
		t.Errorf("mutes from an old file no longer apply: %+v", ms)
	}
}
