package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
)

// archiveFiles lists every archive file of the hour, data files and the
// manifest.
func archiveFiles(t *testing.T, dir string, hour time.Time) []string {
	t.Helper()
	rels, err := archive.LocalDir{Dir: dir}.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	key := "date=" + hour.Format("2006-01-02") + "/hour=" + hour.Format("15") + "."
	var out []string
	for _, r := range rels {
		if strings.Contains(r, key) {
			out = append(out, r)
		}
	}
	return out
}

// After an export, whole hours older than ArchiveRetention go: their data
// files of every export and their manifest. Younger hours stay.
func TestArchiveRetentionPrunesOldHours(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "old"), event(h1.Add(time.Minute), "young"))
	exported(t, a, now)
	// A file of an export from before export ids.
	stray := filepath.Join(dir, "events", "date="+h0.Format("2006-01-02"), "hour="+h0.Format("15")+".parquet")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(archiveFiles(t, dir, h0)) < 3 || len(archiveFiles(t, dir, h1)) < 2 {
		t.Fatalf("export wrote too few files: %v %v", archiveFiles(t, dir, h0), archiveFiles(t, dir, h1))
	}
	a.ArchiveRetention = 2 * time.Hour
	// h0 ended 2h30m before this, h1 1h30m before.
	exported(t, a, h1.Add(150*time.Minute))
	if got := archiveFiles(t, dir, h0); len(got) != 0 {
		t.Errorf("files of an hour past archive retention kept: %v", got)
	}
	got := archiveFiles(t, dir, h1)
	hasManifest := false
	for _, r := range got {
		hasManifest = hasManifest || strings.HasPrefix(r, "manifests/")
	}
	if len(got) < 2 || !hasManifest {
		t.Errorf("files of an hour inside archive retention removed: %v", got)
	}
}

// ArchiveRetention 0 keeps every hour.
func TestArchiveRetentionZeroKeepsAll(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "old"))
	exported(t, a, now)
	before := len(archiveFiles(t, dir, h0))
	exported(t, a, now.Add(365*24*time.Hour))
	if after := len(archiveFiles(t, dir, h0)); after != before || before == 0 {
		t.Fatalf("files %d before, %d after", before, after)
	}
}

// Pruning every hour of a day leaves no empty directory of that day.
func TestArchiveRetentionRemovesEmptyDays(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "old"))
	exported(t, a, now)
	a.ArchiveRetention = time.Hour
	exported(t, a, now.Add(30*24*time.Hour))
	day := "date=" + h0.Format("2006-01-02")
	err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && e.IsDir() && e.Name() == day {
			t.Errorf("empty day directory kept: %s", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
