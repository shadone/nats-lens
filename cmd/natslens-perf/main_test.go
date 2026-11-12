package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/perf"
)

// -interval sets the writer's interval, and with it each write's limit;
// without it the writer keeps the 10 s the recorded baselines used.
func TestIntervalFlagReachesTheWriter(t *testing.T) {
	if testing.Short() {
		t.Skip("generates a store: run without -short")
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "smoke.duckdb")
	if err := run([]string{"generate", "-profile", "smoke", "-out", db}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		flags []string
		want  time.Duration
	}{{nil, perf.DefaultInterval}, {[]string{"-interval", "3s"}, 3 * time.Second}} {
		out := filepath.Join(dir, "load.json")
		args := append([]string{"load", "-profile", "smoke", "-db", db, "-scenarios", "writes-only", "-duration", "2s", "-json", out}, c.flags...)
		if err := run(args); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		var rep perf.Report
		if err := json.Unmarshal(b, &rep); err != nil {
			t.Fatal(err)
		}
		if len(rep.Results) != 1 || rep.Results[0].Background == nil {
			t.Fatalf("flags %v: results %+v", c.flags, rep.Results)
		}
		if got := rep.Results[0].Background.EpochInterval; got != c.want {
			t.Errorf("flags %v: the writer's interval is %s, want %s", c.flags, got, c.want)
		}
	}
}
