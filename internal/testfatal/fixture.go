// Package testfatal supplies a disposable, synthetic pre-existing index failure.
package testfatal

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"os"
	"testing"
)

// Created only from synthetic SQL with DuckDB 1.5.6; see testdata/README.md.
// Keeping the old file tests real typed fatal containment even when the engine
// prevents its creation. An upgrade does not repair already-divergent indexes.
//
//go:embed testdata/index-divergence-1.5.6.duckdb.gz
var divergentFixture []byte

// Prepare writes a fresh copy of the immutable fixture. The supplied path must
// be inside a fresh test temporary directory. No process opens the source file.
func Prepare(t *testing.T, path string) {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(divergentFixture))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
