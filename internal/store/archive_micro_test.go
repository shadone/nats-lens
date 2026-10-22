package store_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// The archive carries the micro service tables through export and
// rebuild, and an archive written before migration 0033, without their
// files, still rebuilds.
func TestArchiveMicro(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	if _, err := st.WriteSnapshot(t.Context(), store.FixtureSnapshot(t, h0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d hours", n)
	}
	rb := rebuilt(t, dir, now)
	for table, want := range map[string]int{"micro_requests": 4, "micro_instances": 2, "micro_stats": 2, "micro_endpoints": 4,
		"micro_endpoint_hours": 2} {
		if n := scalar[int](t, rb, `SELECT count(*) FROM `+table); n != want {
			t.Errorf("rebuilt %s = %d, want %d", table, n, want)
		}
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM micro_endpoints
WHERE last_error = '409 duplicate order id' AND json_extract_string(data, '$.retries') = '3'`); n != 1 {
		t.Errorf("rebuilt endpoint with last error and data = %d, want 1", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM micro_instances WHERE json_extract_string(endpoints, '$[1].subject') = 'orders.get.*'`); n != 2 {
		t.Errorf("rebuilt instance endpoints = %d, want 2", n)
	}

	micro := []string{"micro_requests", "micro_instances", "micro_stats", "micro_endpoints", "micro_endpoint_hours"}
	dropFromManifests(t, dir, micro...)
	rb = rebuilt(t, dir, now)
	for _, table := range micro {
		if n := scalar[int](t, rb, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("rebuild from an archive before 0033 has %d %s rows", n, table)
		}
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM subscriptions`); n != 5 {
		t.Errorf("rebuild from an archive before 0033: subscriptions = %d, want 5", n)
	}
}
