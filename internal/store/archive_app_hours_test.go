package store_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// The archive carries client_app_hours through export and rebuild, and an
// archive written before migration 0034, without its files, still
// rebuilds.
func TestArchiveClientAppHours(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	start := h0.Add(-time.Hour)
	for i, n := range []int64{3, 10} {
		snap := store.FixtureSnapshot(t, h0.Add(time.Duration(i+1)*time.Minute))
		for _, v := range snap.Servers {
			c := v.Connz.Conns[0]
			c.Start, c.InMsgs, c.OutMsgs, c.InBytes, c.OutBytes = start, n, 2*n, 10*n, 20*n
		}
		if _, err := st.WriteSnapshot(t.Context(), snap); err != nil {
			t.Fatal(err)
		}
	}
	const q = `SELECT strftime(hour, '%Y-%m-%d %H') || ' ' || account || ' ' || app || ' ' || loopback || ' ' ||
  in_msgs || ' ' || out_msgs || ' ' || in_bytes || ' ' || out_bytes || ' ' || epochs FROM client_app_hours`
	want := "2026-10-16 22 APP media-worker false 14 28 140 280 1"
	if got := scalar[string](t, st, q); got != want {
		t.Fatalf("client_app_hours = %q, want %q", got, want)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d hours", n)
	}
	if got := scalar[string](t, rebuilt(t, dir, now), q); got != want {
		t.Errorf("rebuilt client_app_hours = %q, want %q", got, want)
	}

	dropFromManifests(t, dir, "client_app_hours")
	rb := rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT count(*) FROM client_app_hours`); n != 0 {
		t.Errorf("rebuild from an archive before 0034 has %d client_app_hours rows", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM connections`); n != 4 {
		t.Errorf("rebuild from an archive before 0034: connections = %d, want 4", n)
	}
}
