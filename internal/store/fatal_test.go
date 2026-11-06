package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// Separate processes exercise the pinned engine's WAL replay/shutdown boundary.
// Only disposable synthetic data is used; this says nothing about other files.
func TestFatalEngineContainment(t *testing.T) {
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
		return
	}
	path := filepath.Join(t.TempDir(), "synthetic.duckdb")
	for _, mode := range []string{"create", "replay"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFatalEngineContainment$")
		cmd.Env = append(os.Environ(), "NATSLENS_FATAL_FIXTURE="+mode, "NATSLENS_FATAL_PATH="+path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture %s: %v: %s", mode, err, out)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tx, err := st.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM fatal_fixture"); err != nil {
		t.Fatal(err)
	}
	err = tx.Commit()
	var de *duckdb.Error
	if !errors.As(fmt.Errorf("wrapped: %w", err), &de) || de.Type != duckdb.ErrorTypeFatal {
		t.Fatalf("expected typed fatal, got %T", err)
	}
	if strings.Contains(err.Error(), "Chunk") || len(err.Error()) > 512 {
		t.Errorf("fatal diagnostic leaked row chunks (%d bytes)", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "commit") {
		t.Error("fatal error lacks operation stage")
	}
	// Both reads and writes must be refused with the original bounded failure.
	for _, q := range []string{"SELECT 1", "CREATE TABLE must_not_run(i INT)"} {
		_, next := st.DB().ExecContext(ctx, q)
		if next == nil || next.Error() != err.Error() {
			t.Error("failed engine was used again rather than returning its latched failure")
		}
	}
	healthy, err := OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer healthy.Close()
	if _, err := healthy.DB().ExecContext(ctx, "SELECT 1"); err != nil {
		t.Fatal("separate engine affected:", err)
	}
}
