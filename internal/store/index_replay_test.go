package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// Exercise the engine directly: OpenPath's explicit startup CHECKPOINT would
// mask the context-free shutdown checkpoint in duckdb/duckdb#25771.
// Every stage must be a separate process, including the full scan after commit.
func TestIndexSurvivesWALReplayShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.duckdb")
	run := func(stage string) error {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// ASVS 1.2.5: pass arguments directly; no shell or external fixture.
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIndexReplayProcess$", "-test.v")
		cmd.Env = append(os.Environ(), "NATSLENS_INDEX_STAGE="+stage, "NATSLENS_INDEX_PATH="+path)
		out, err := cmd.CombinedOutput()
		t.Logf("%s: %s", stage, out)
		return err
	}
	for _, stage := range []string{"create", "replay"} {
		if err := run(stage); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
	}
	deleteErr := run("delete")
	// Still inspect the persisted rows if COMMIT failed, using a fresh engine.
	scanErr := run("scan")
	if deleteErr != nil || scanErr != nil {
		t.Fatalf("committed delete: %v; reopened full scan: %v", deleteErr, scanErr)
	}
}

func TestIndexReplayProcess(t *testing.T) {
	stage := os.Getenv("NATSLENS_INDEX_STAGE")
	if stage == "" {
		return
	}
	db, err := sql.Open("duckdb", os.Getenv("NATSLENS_INDEX_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	var version string
	if err := db.QueryRow("SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("engine %s", version)
	execSQL := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(SafeError(stage, err))
		}
	}
	switch stage {
	case "create":
		execSQL(`CREATE TABLE epoch_endpoints(epoch BIGINT NOT NULL, server_id VARCHAR NOT NULL, endpoint VARCHAR NOT NULL, state VARCHAR NOT NULL, error VARCHAR);
CREATE UNIQUE INDEX epoch_endpoints_key ON epoch_endpoints(epoch, server_id, endpoint)`)
		execSQL(`INSERT INTO epoch_endpoints SELECT i, 'server-a', 'STATSZ', 'ok', NULL FROM range(1,1751) t(i)`)
		execSQL("CHECKPOINT")
		execSQL(`INSERT INTO epoch_endpoints SELECT i, 'server-a', 'STATSZ', 'ok', NULL FROM range(1751,2049) t(i)`)
		execSQL("PRAGMA disable_checkpoint_on_shutdown")
	case "replay":
		// Open replays the 298 appended rows. Close with no explicit CHECKPOINT
		// and no query that binds the unique index before shutdown.
		execSQL("SELECT 1")
	case "delete":
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec("DELETE FROM epoch_endpoints"); err != nil {
			t.Fatal(SafeError("delete", err))
		}
		t.Log("DELETE Exec succeeded")
		if err := tx.Commit(); err != nil {
			var de *duckdb.Error
			t.Fatalf("COMMIT: %v; typed fatal=%t", SafeError("commit", err), errors.As(err, &de) && de.Type == duckdb.ErrorTypeFatal)
		}
		t.Log("COMMIT succeeded")
	case "scan":
		execSQL("PRAGMA disable_optimizer")
		var n int
		if err := db.QueryRow("SELECT count(*) FROM epoch_endpoints").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("reopened full scan rows=%d, want 0", n)
		}
		// The index must still reject duplicates after delete and reopen.
		execSQL(`INSERT INTO epoch_endpoints VALUES (1, 'server-a', 'STATSZ', 'ok', NULL)`)
		if _, err := db.Exec(`INSERT INTO epoch_endpoints VALUES (1, 'server-a', 'STATSZ', 'ok', NULL)`); err == nil {
			t.Fatal("unique index accepted a duplicate")
		}
		execSQL("CHECKPOINT")
	default:
		t.Fatalf("unknown stage %q", stage)
	}
}
