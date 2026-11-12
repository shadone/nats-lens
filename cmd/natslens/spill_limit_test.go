package main

import (
	"context"
	"testing"
	"time"
)

// Each system's database gets its spill bound when it opens: its own
// duckdb_max_temp_size, else twice its share of the memory budget.
func TestOpenSystemsBoundTheSpill(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	cfg.DuckDBMemory = 600 << 20
	cfg.Systems[0].DuckDBMaxTemp = 100 << 20
	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSystems(rts, time.Time{})
	want := map[string]string{"shop-prod": "100.0 MiB", "shop-staging": "600.0 MiB"}
	for _, rt := range rts {
		var got string
		if err := rt.st.DB().QueryRow(`SELECT current_setting('max_temp_directory_size')`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want[rt.sys.ID] {
			t.Errorf("%s: max_temp_directory_size = %q, want %q", rt.sys.ID, got, want[rt.sys.ID])
		}
	}
}
