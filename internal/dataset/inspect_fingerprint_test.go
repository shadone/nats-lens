package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

func TestInspectAndFingerprintClosedDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "closed.duckdb")
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := OpenDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err := InspectDatabase(ctx, db)
	if err != nil || b.DataReference != nil {
		t.Fatalf("empty boundaries %+v %v", b, err)
	}
	got, err := FingerprintDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint %s", got)
	}
	if err = db.PingContext(ctx); err != nil {
		t.Fatal("borrowed DB closed")
	}
	if _, err = InspectDatabase(ctx, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = InspectDatabase(cancelled, db); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = FingerprintDatabase(cancelled, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = FingerprintDatabase(ctx, filepath.Dir(path)); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	large := filepath.Join(t.TempDir(), "oversized.duckdb")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxDataBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if _, err = FingerprintDatabase(ctx, large); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
}
