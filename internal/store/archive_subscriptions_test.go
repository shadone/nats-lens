package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
)

// dropFromManifests rewrites every manifest in dir without the files of
// tables and removes those files, as an archive written before the tables
// existed.
func dropFromManifests(t *testing.T, dir string, tables ...string) {
	t.Helper()
	var manifests []string
	err := filepath.WalkDir(filepath.Join(dir, "manifests"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			manifests = append(manifests, p)
		}
		return err
	})
	if err != nil || len(manifests) == 0 {
		t.Fatalf("manifests %v, err %v", manifests, err)
	}
	for _, p := range manifests {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var m archive.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		m.Files = slices.DeleteFunc(m.Files, func(f archive.ManifestFile) bool { return slices.Contains(tables, f.Table) })
		if b, err = json.Marshal(m); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range tables {
		if err := os.RemoveAll(filepath.Join(dir, table)); err != nil {
			t.Fatal(err)
		}
	}
}

// The archive carries the subscription lists through export and rebuild,
// and an archive written before migration 0032, without their files,
// still rebuilds.
func TestArchiveSubscriptions(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	if _, err := st.WriteSnapshot(t.Context(), store.FixtureSnapshot(t, h0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d hours", n)
	}
	rb := rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT count(*) FROM subscriptions`); n != 5 {
		t.Errorf("rebuilt subscriptions = %d, want 5", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM subscriptions WHERE queue = 'media-worker' AND subs = 2 AND msgs = 7`); n != 1 {
		t.Errorf("rebuilt folded queue row = %d, want 1", n)
	}
	if n := scalar[int](t, rb, `SELECT sum(total) FROM subscription_lists`); n != 8 {
		t.Errorf("rebuilt subscription_lists total = %d, want 8", n)
	}

	dropFromManifests(t, dir, "subscriptions", "subscription_lists")
	rb = rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT (SELECT count(*) FROM subscriptions) + (SELECT count(*) FROM subscription_lists)`); n != 0 {
		t.Errorf("rebuild from an archive before 0032 has %d subscription rows", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM connections`); n != 2 {
		t.Errorf("rebuild from an archive before 0032: connections = %d, want 2", n)
	}
}
