// Package testfatal builds a disposable WAL fixture for the pinned engine.
package testfatal

import (
	"database/sql"
	"os"
	"os/exec"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

// Prepare returns true in the fixture subprocess, whose caller must return.
// The supplied path must be inside a fresh test temporary directory.
func Prepare(t *testing.T, path string) bool {
	if mode := os.Getenv("NATSLENS_FATAL_FIXTURE"); mode != "" {
		db, err := sql.Open("duckdb", os.Getenv("NATSLENS_FATAL_PATH"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		queries := []string{"SELECT 1"}
		if mode == "create" {
			queries = []string{
				`CREATE TABLE fatal_fixture(epoch BIGINT, server_id VARCHAR, endpoint VARCHAR, state VARCHAR NOT NULL)`,
				`CREATE UNIQUE INDEX fatal_fixture_key ON fatal_fixture(epoch,server_id,endpoint)`,
				`INSERT INTO fatal_fixture SELECT i,'server-a','STATSZ','ok' FROM range(1,1751)t(i)`,
				`CHECKPOINT`,
				`INSERT INTO fatal_fixture SELECT i,'server-a','STATSZ','ok' FROM range(1751,2049)t(i)`,
				`PRAGMA disable_checkpoint_on_shutdown`,
			}
		}
		for _, q := range queries {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		return true
	}

	for _, mode := range []string{"create", "replay"} {
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
		cmd.Env = append(os.Environ(), "NATSLENS_FATAL_FIXTURE="+mode, "NATSLENS_FATAL_PATH="+path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture %s: %v: %s", mode, err, out)
		}
	}
	return false
}
