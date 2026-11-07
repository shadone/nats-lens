package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/shadone/nats-lens/internal/testfatal"
)

// A pre-existing synthetic index failure must still produce a typed fatal after
// an engine upgrade. Only disposable data is used.
func TestFatalEngineContainment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.duckdb")
	testfatal.Prepare(t, path)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Failure() != nil || !st.FailedAt().IsZero() {
		t.Fatal("new store is unhealthy")
	}
	firstPossibleFailure := time.Now()
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
	if st.Failure() != err || st.FailedAt().Before(firstPossibleFailure) || st.FailedAt().After(time.Now()) {
		t.Error("first fatal state was not retained")
	}
	select {
	case <-st.Failed():
	default:
		t.Error("fatal did not signal runtime")
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
