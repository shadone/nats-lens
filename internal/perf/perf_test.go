package perf

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// The smoke profile generates, explains and serves its scenarios without
// an error. It checks the harness, not any timing.
func TestSmokeProfileRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("generates a store: run without -short")
	}
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	p := Profiles["smoke"]
	path := filepath.Join(t.TempDir(), "smoke.duckdb")
	res, err := Generate(ctx, p, path, log)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows["epochs"] != int64(p.Epochs()) || res.Rows["consumer_stats"] == 0 || res.Rows["events"] == 0 {
		t.Fatalf("rows %v", res.Rows)
	}
	if err := Explain(ctx, path, p, t.TempDir(), log); err != nil {
		t.Fatal(err)
	}
	rep, err := Load(ctx, LoadOptions{Profile: p, Path: path, Scenarios: []string{"cold-overview", "agents-1", "writes-only"},
		Duration: 2 * time.Second, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Results {
		for _, pr := range r.Procedures {
			if pr.Errors > 0 {
				t.Errorf("%s: %s failed: %s", r.Name, pr.Procedure, pr.FirstError)
			}
		}
		if b := r.Background; b != nil && (b.Epochs == 0 || len(b.Errors) > 0) {
			t.Errorf("%s: background %+v", r.Name, b)
		}
	}
}
