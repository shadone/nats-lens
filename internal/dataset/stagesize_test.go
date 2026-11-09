package dataset

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// vanishingFS lists every file of MapFS but reports gone, when its size is
// read, the one named gone: DuckDB removed it between the two steps.
type vanishingFS struct {
	fstest.MapFS
	gone string
}

func (v vanishingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := v.MapFS.ReadDir(name)
	for i, e := range entries {
		if e.Name() == v.gone {
			entries[i] = goneEntry{e}
		}
	}
	return entries, err
}

type goneEntry struct{ fs.DirEntry }

func (goneEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }

func TestStageSizeSkipsAFileRemovedDuringTheWalk(t *testing.T) {
	fsys := vanishingFS{MapFS: fstest.MapFS{
		"data.duckdb":     {Data: make([]byte, 100)},
		"data.duckdb.wal": {Data: make([]byte, 7)},
		"part/a.parquet":  {Data: make([]byte, 20)},
	}, gone: "data.duckdb.wal"}
	size, err := stageSize(fsys)
	if err != nil {
		t.Fatalf("stageSize: %v", err)
	}
	if size != 120 {
		t.Fatalf("size %d, want 120", size)
	}
}

func TestStageSizeFailsWhenTheStageIsGone(t *testing.T) {
	if _, err := stageSize(os.DirFS(filepath.Join(t.TempDir(), "missing"))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err %v, want fs.ErrNotExist", err)
	}
}

// deniedFS refuses to list one directory.
type deniedFS struct {
	fstest.MapFS
	denied string
}

func (d deniedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == d.denied {
		return nil, fs.ErrPermission
	}
	return d.MapFS.ReadDir(name)
}

// Only a vanished file is skipped: any other error still stops the export.
func TestStageSizeReturnsOtherErrors(t *testing.T) {
	fsys := deniedFS{MapFS: fstest.MapFS{"part/a.parquet": {Data: []byte("x")}}, denied: "part"}
	if _, err := stageSize(fsys); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err %v, want fs.ErrPermission", err)
	}
}
