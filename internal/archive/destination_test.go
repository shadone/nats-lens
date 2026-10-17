package archive

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestRelPathRoundTrip(t *testing.T) {
	h := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	id := ExportID(time.Date(2026, 10, 17, 8, 1, 2, 3, time.UTC))
	rel := RelPath("consumer_stats", h, id)
	if rel != "consumer_stats/date=2026-10-17/hour=07.20261017T080102.000000003Z.parquet" {
		t.Fatalf("rel = %s", rel)
	}
	table, hour, gotID, ok := ParseRelPath(rel)
	if !ok || table != "consumer_stats" || !hour.Equal(h) || gotID != id {
		t.Fatalf("parsed %s %v %s %v", table, hour, gotID, ok)
	}
	// Files written before export ids have none.
	if _, hour, gotID, ok := ParseRelPath("events/date=2026-10-17/hour=07.parquet"); !ok || !hour.Equal(h) || gotID != "" {
		t.Fatalf("legacy name: %v %q %v", hour, gotID, ok)
	}
	for _, bad := range []string{"x.parquet", "t/date=2026-10-17/hour=07.csv", "t/day=2026-10-17/hour=07.parquet", "t/date=2026-13-17/hour=07.parquet", "t/date=2026-10-17/hour=7.parquet"} {
		if _, _, _, ok := ParseRelPath(bad); ok {
			t.Errorf("%s parsed", bad)
		}
	}
}

func TestLocalDirPutAndList(t *testing.T) {
	ctx := context.Background()
	d := LocalDir{Dir: filepath.Join(t.TempDir(), "archive")}
	if got, err := d.List(ctx, ""); err != nil || len(got) != 0 {
		t.Fatalf("missing dir: %v %v", got, err)
	}
	if err := os.MkdirAll(d.StageDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(d.StageDir(), "f.tmp")
	if err := os.WriteFile(staged, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	rel := "events/date=2026-10-17/hour=07.parquet"
	if err := d.Put(ctx, rel, staged); err != nil {
		t.Fatal(err)
	}
	// A file still in staging is not part of the archive.
	if err := os.WriteFile(filepath.Join(d.StageDir(), "half.tmp"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := d.List(ctx, "")
	if err != nil || !slices.Equal(got, []string{rel}) {
		t.Fatalf("list = %v %v", got, err)
	}
	p, _ := d.Fetch(ctx, rel)
	if b, err := os.ReadFile(p); err != nil || string(b) != "a" {
		t.Fatalf("fetch: %q %v", b, err)
	}
}

func TestManifestRelPath(t *testing.T) {
	h := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	rel := ManifestRelPath(h)
	if rel != "manifests/date=2026-10-17/hour=07.json" {
		t.Fatalf("rel = %s", rel)
	}
	if got, ok := parseManifestRelPath(rel); !ok || !got.Equal(h) {
		t.Fatalf("parsed %v %v", got, ok)
	}
	if _, ok := parseManifestRelPath("events/date=2026-10-17/hour=07.parquet"); ok {
		t.Fatal("a data file parsed as a manifest")
	}
}

func TestLocalDirListPrefixAndRemove(t *testing.T) {
	ctx := context.Background()
	d := LocalDir{Dir: t.TempDir()}
	h := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	rels := []string{RelPath("events", h, "a"), RelPath("events", h, "b"), RelPath("events", h.Add(time.Hour), "a"), RelPath("epochs", h, "a")}
	for _, rel := range rels {
		p := filepath.Join(d.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.List(ctx, hourPrefix("events", h))
	if err != nil || !slices.Equal(got, rels[:2]) {
		t.Fatalf("list = %v %v", got, err)
	}
	if err := d.Remove(ctx, rels[0]); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove(ctx, rels[0]); err != nil {
		t.Fatalf("removing a missing file: %v", err)
	}
	if got, _ := d.List(ctx, "events/"); len(got) != 2 {
		t.Fatalf("after remove: %v", got)
	}
}

// Paths that leave the archive directory are refused.
func TestLocalDirRejectsNonLocalPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	d := LocalDir{Dir: filepath.Join(root, "archive")}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../outside", "/etc/passwd", "a/../../outside"} {
		if err := d.Remove(ctx, rel); err == nil {
			t.Errorf("Remove(%q) accepted", rel)
		}
		if _, err := d.Fetch(ctx, rel); err == nil {
			t.Errorf("Fetch(%q) accepted", rel)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the archive removed: %v", err)
	}
}
