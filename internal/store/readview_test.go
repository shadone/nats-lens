package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestReadViewEnforcesBorrowedReadOnly(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "closed.duckdb")
	st, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewReadView(ctx, st.DB()); err == nil {
		t.Fatal("accepted writable DB")
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	view, err := NewReadView(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = view.DB().ExecContext(ctx, "INSERT INTO epochs (epoch) VALUES (999)"); err == nil {
		t.Fatal("write permitted")
	}
	if err = view.Close(); err != nil {
		t.Fatal(err)
	}
	if err = db.PingContext(ctx); err != nil {
		t.Fatalf("borrowed handle closed: %v", err)
	}
	if view.Failure() != nil || !view.FailedAt().IsZero() || view.Failed() != nil {
		t.Fatal("unexpected guard state")
	}
}
