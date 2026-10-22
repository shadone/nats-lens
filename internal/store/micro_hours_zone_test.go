package store

import (
	"context"
	"testing"
	"time"
)

// The hourly sums and their sweep do not depend on the database's time
// zone: with TimeZone set to zones west and east of UTC, the same epochs
// give the same hours as in UTC, and the sweep keeps the same hours.
func TestMicroEndpointHoursAnyTimeZone(t *testing.T) {
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	old := h.Add(-24 * time.Hour)
	run := func(zone string) ([]string, []string) {
		s := openMem(t)
		ctx := context.Background()
		if _, err := s.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
			t.Fatal(err)
		}
		for i := range 4 {
			at := h.Add(59*time.Minute + time.Duration(i)*30*time.Second)
			// B started between the first and second epoch: its first
			// row counts whole.
			snap := hourSnapshot(t, at, hourInstance{"A", old, 100 + 10*i, 0, time.Duration(100+i) * time.Millisecond},
				hourInstance{"B", h.Add(59*time.Minute + 10*time.Second), 4 + i, 0, time.Millisecond})
			if _, err := s.WriteSnapshot(ctx, snap); err != nil {
				t.Fatal(err)
			}
		}
		q := `SELECT strftime(hour, '%Y-%m-%d %H'), requests, processing_time_ns, epochs FROM micro_endpoint_hours ORDER BY hour`
		before := rowStrings(t, s.DB(), q)
		if _, err := s.Sweep(ctx, h.Add(2*time.Hour+30*time.Minute), time.Hour); err != nil {
			t.Fatal(err)
		}
		return before, rowStrings(t, s.DB(), q)
	}
	wantBefore, wantAfter := run("UTC")
	if len(wantBefore) != 2 || len(wantAfter) != 1 {
		t.Fatalf("UTC: hours %v, after the sweep %v", wantBefore, wantAfter)
	}
	for _, zone := range []string{"America/Los_Angeles", "Pacific/Auckland"} {
		before, after := run(zone)
		wantRows(t, zone+" hours", before, wantBefore)
		wantRows(t, zone+" hours after the sweep", after, wantAfter)
	}
}
