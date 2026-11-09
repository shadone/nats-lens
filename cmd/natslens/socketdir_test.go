package main

import (
	"os"
	"path/filepath"
	"testing"
)

// shortSocketDir makes a directory under /tmp for a Unix socket: the
// directory t.TempDir makes on macOS is too long for a socket path, which
// macOS limits to 104 bytes. Symlinks are resolved (macOS's /tmp points to
// /private/tmp) so that the directory matches the canonical paths the
// dataset writer returns. It is removed when the test ends.
func shortSocketDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}
