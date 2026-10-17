// Package archive writes each hour of the database to Parquet files,
// <table>/date=YYYY-MM-DD/hour=HH.<export>.parquet listed by a manifest per
// hour, and rebuilds a missing database from them. The database is a cache;
// the archive is the durable history.
package archive

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Destination is where archive files live. LocalDir is the local
// implementation; a bucket uploader implements the same methods.
type Destination interface {
	// StageDir returns a local directory for files about to be Put.
	StageDir() string
	// Put stores the local file at path under rel, replacing any file
	// there, and removes path. A reader never sees a half-written file.
	Put(ctx context.Context, rel, path string) error
	// List returns the relative paths of the archive files that start with
	// prefix; an empty prefix lists every file.
	List(ctx context.Context, prefix string) ([]string, error)
	// Remove deletes the file at rel; a missing file is no error.
	Remove(ctx context.Context, rel string) error
	// ModTime returns when the file at rel was last written.
	ModTime(ctx context.Context, rel string) (time.Time, error)
	// Fetch returns a local path DuckDB can read for rel; a remote
	// destination downloads the file first.
	Fetch(ctx context.Context, rel string) (string, error)
}

// LocalDir is an archive in a local directory.
type LocalDir struct {
	Dir string
}

// stageName is the staging directory inside the archive directory, on the
// same file system so that Put is a rename.
const stageName = ".staging"

// StageDir implements Destination.
func (d LocalDir) StageDir() string { return filepath.Join(d.Dir, stageName) }

// Put implements Destination by renaming the staged file into place. The
// file is synced before the rename and the directory after it, so that a
// crash leaves either the old file or the complete new one.
func (d LocalDir) Put(_ context.Context, rel, path string) error {
	dst, err := d.local(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := syncPath(path); err != nil {
		return fmt.Errorf("archive %s: sync: %w", rel, err)
	}
	if err := os.Rename(path, dst); err != nil {
		return fmt.Errorf("archive %s: %w", rel, err)
	}
	if err := syncPath(filepath.Dir(dst)); err != nil {
		return fmt.Errorf("archive %s: sync directory: %w", rel, err)
	}
	return nil
}

// syncPath flushes a file or directory to disk.
func syncPath(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// List implements Destination. It walks only the directory the prefix
// names, so listing one hour's files stays cheap.
func (d LocalDir) List(_ context.Context, prefix string) ([]string, error) {
	root := d.Dir
	if i := strings.LastIndex(prefix, "/"); i >= 0 {
		root = filepath.Join(d.Dir, filepath.FromSlash(prefix[:i]))
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == root {
				return fs.SkipAll
			}
			return err
		}
		if e.IsDir() {
			if e.Name() == stageName {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(d.Dir, p)
		if err != nil {
			return err
		}
		if rel = filepath.ToSlash(rel); strings.HasPrefix(rel, prefix) {
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

// Remove implements Destination and syncs the directory.
func (d LocalDir) Remove(_ context.Context, rel string) error {
	p, err := d.local(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return syncPath(filepath.Dir(p))
}

// ModTime implements Destination.
func (d LocalDir) ModTime(_ context.Context, rel string) (time.Time, error) {
	p, err := d.local(rel)
	if err != nil {
		return time.Time{}, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

// Fetch implements Destination; local files are read in place.
func (d LocalDir) Fetch(_ context.Context, rel string) (string, error) {
	return d.local(rel)
}

// local returns the file path of rel inside the archive directory, and an
// error for a path that is absolute or leaves the directory.
func (d LocalDir) local(rel string) (string, error) {
	p := filepath.FromSlash(rel)
	if !filepath.IsLocal(p) {
		return "", fmt.Errorf("archive path %q is not inside the archive", rel)
	}
	return filepath.Join(d.Dir, p), nil
}
