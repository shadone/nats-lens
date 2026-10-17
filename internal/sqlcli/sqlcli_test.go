package sqlcli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

func TestRunReadsAClosedDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = Run(ctx, filepath.Join(dir, store.FileName), `SELECT version, name FROM migrations ORDER BY version LIMIT 2`, &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "epochs_servers") || !strings.Contains(got, "(2 rows)") {
		t.Fatalf("output:\n%s", got)
	}
}

func TestRunRejectsWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	var out bytes.Buffer
	if err := Run(ctx, filepath.Join(dir, store.FileName), `DELETE FROM migrations`, &out); err == nil {
		t.Fatal("a read-only database must reject writes")
	}
}
