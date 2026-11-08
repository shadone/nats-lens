package main

import (
	"bytes"
	"context"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLBundleSelectionRejectsAmbiguity(t *testing.T) {
	for _, args := range [][]string{{"-bundle", "fixture", "-db", "closed.duckdb", "SELECT 1"}, {"-bundle", "fixture", "-config", "missing", "SELECT 1"}} {
		var out bytes.Buffer
		err := runSQLTo(context.Background(), args, &out)
		if err == nil || !strings.Contains(err.Error(), "exclusive") || out.Len() != 0 {
			t.Fatalf("%v: %v %s", args, err, out.String())
		}
	}
}

func TestSQLCanonicalBundle(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, filepath.Join(t.TempDir(), "source.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	snap := collect.NewSnapshot(time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC))
	snap.FinishedAt = snap.StartedAt.Add(time.Second)
	if _, err = st.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "capture")
	_, err = dataset.Acquire(ctx, st, path, dataset.System{ID: "shop", CollectionIntervalNS: int64(30 * time.Second)}, "fixture", dataset.Options{MaxBytes: 64 << 20})
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = runSQLTo(ctx, []string{"-bundle", path, "-format", "json", "SELECT epoch FROM epochs"}, &out); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	if !strings.Contains(out.String(), `"kind":"bundle"`) || !strings.Contains(out.String(), `"manifest":`) {
		t.Fatalf("validated source provenance missing: %s", out.String())
	}
	if !strings.Contains(out.String(), `"BIGINT"`) || !strings.Contains(out.String(), `"1"`) {
		t.Fatalf("typed result missing: %s", out.String())
	}
	stage := filepath.Join(root, ".dataset-partial-unpublished")
	if err = os.Rename(path, stage); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = runSQLTo(ctx, []string{"-bundle", stage, "-format", "json", "SELECT epoch FROM epochs"}, &out); err == nil || out.Len() != 0 {
		t.Fatalf("unpublished SQL bundle exposed: %v %s", err, out.String())
	}

}
