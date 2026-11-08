package dataset

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
)

func synthetic(t *testing.T) (*store.Store, System) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	at := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	snap := collect.NewSnapshot(at)
	snap.FinishedAt = at.Add(time.Second)
	if _, err = st.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	return st, System{ID: "shop-prod", Name: "Shop production", CollectionIntervalNS: int64(30 * time.Second)}
}
func acquireFixture(t *testing.T) (string, Manifest) {
	t.Helper()
	st, sys := synthetic(t)
	path := filepath.Join(t.TempDir(), "capture")
	m, err := Acquire(context.Background(), st, path, sys, "test", Options{})
	if err != nil {
		t.Fatal(err)
	}
	return path, m
}
func TestAcquireAndOpen(t *testing.T) {
	path, m := acquireFixture(t)
	d, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.Manifest.Boundaries.LatestEpoch == nil || *d.Manifest.Boundaries.LatestEpoch != 1 || m.Boundaries.DataReference == nil || m.Boundaries.DataReference.Second() != 1 {
		t.Fatalf("boundaries %+v", m.Boundaries)
	}
	if _, err = d.DB.Exec("CREATE TABLE accidental_write(i INT)"); err == nil {
		t.Fatal("dataset allows writes")
	}
	if _, err = d.DB.Exec("SELECT * FROM read_csv('/etc/passwd')"); err == nil {
		t.Fatal("external access enabled")
	}
	if m.Boundaries.PendingEvaluationEpochs != 1 {
		t.Fatal("unpublished evaluations hidden")
	}
}
func TestDatasetRejectsDamage(t *testing.T) {
	path, _ := acquireFixture(t)
	p := filepath.Join(path, DataFile)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("damage"))
	f.Close()
	if _, err = Open(context.Background(), path); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("got %v", err)
	}
}
func TestDatasetRejectsManifestPathsAndVersions(t *testing.T) {
	for _, which := range []string{"path", "version", "partial", "provenance"} {
		t.Run(which, func(t *testing.T) {
			path, m := acquireFixture(t)
			switch which {
			case "path":
				m.Data.Path = "../source.duckdb"
			case "version":
				m.FormatVersion++
			case "partial":
				m.Complete = false
			case "provenance":
				v := int64(999)
				m.Boundaries.LatestEpoch = &v
			}
			b, _ := json.Marshal(m)
			if err := os.WriteFile(filepath.Join(path, ManifestFile), b, 0600); err != nil {
				t.Fatal(err)
			}
			if d, err := Open(context.Background(), path); err == nil {
				d.Close()
				t.Fatal("accepted invalid dataset")
			}
		})
	}
}
