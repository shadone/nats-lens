package dataset

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	duckdb "github.com/duckdb/duckdb-go/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/events"
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
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "capture")
	m, err := Acquire(context.Background(), st, path, sys, "test", Options{MaxBytes: 64 << 20})
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

func privateRoot(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	if err := os.Chmod(p, 0700); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestAcquireFailureDoesNotPublishOrOverwrite(t *testing.T) {
	st, sys := synthetic(t)
	root := privateRoot(t)
	dest := filepath.Join(root, "capture")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, st, dest, sys, "test", Options{MaxBytes: 64 << 20}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("cancelled artifact published")
	}
	if _, err := Acquire(context.Background(), st, dest, sys, "test", Options{MaxBytes: 1}); !errors.Is(err, ErrBudget) {
		t.Fatalf("oversize: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("oversize artifact published")
	}
	// Force admission failure without filling the host's disk.
	if _, err := Acquire(context.Background(), st, dest, sys, "test", Options{MaxBytes: 64 << 20, MinFreeBytes: MaxDataBytes + 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid headroom: %v", err)
	}
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dest, "unrelated")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(context.Background(), st, dest, sys, "test", Options{MaxBytes: 64 << 20}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "keep" {
		t.Fatal("overwrote unrelated file")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("owned failed staging not cleaned: %v %v", entries, err)
	}
	snap := collect.NewSnapshot(time.Now().UTC())
	snap.FinishedAt = snap.StartedAt.Add(time.Second)
	if _, err = st.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("collection stopped: %v", err)
	}
}
func TestDatasetRejectsSymlinksAndUnexpectedFiles(t *testing.T) {
	for _, which := range []string{"file", "directory", "partial"} {
		t.Run(which, func(t *testing.T) {
			path, _ := acquireFixture(t)
			switch which {
			case "file":
				p := filepath.Join(path, DataFile)
				if err := os.Rename(p, p+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+".saved", p); err != nil {
					t.Fatal(err)
				}
			case "directory":
				link := filepath.Join(privateRoot(t), "link")
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			case "partial":
				if err := os.WriteFile(filepath.Join(path, "data.duckdb.wal"), []byte("partial"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if d, err := Open(context.Background(), path); err == nil {
				d.Close()
				t.Fatal("unsafe/partial dataset opened")
			}
		})
	}
}
func TestOpenDatabaseRejectsWALAndSchemaDamageWithoutMutation(t *testing.T) {
	path, _ := acquireFixture(t)
	dbPath := filepath.Join(path, DataFile)
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dbPath+".wal", []byte("synthetic orphan WAL"), 0600); err != nil {
		t.Fatal(err)
	}
	if db, err := OpenDatabase(context.Background(), dbPath); err == nil {
		db.Close()
		t.Fatal("WAL source opened")
	}
	after, _ := os.ReadFile(dbPath)
	if string(before) != string(after) {
		t.Fatal("source mutated")
	}
	if err = os.Remove(dbPath + ".wal"); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDatabase(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.WriteFile(filepath.Join(path, ManifestFile), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(context.Background(), path); err == nil {
		t.Fatal("empty manifest accepted")
	}
}

func TestAcquireInsufficientSpace(t *testing.T) {
	st, sys := synthetic(t)
	root := privateRoot(t)
	dest := filepath.Join(root, "capture")
	old := availableBytes
	availableBytes = func(string) (int64, error) { return 1, nil }
	defer func() { availableBytes = old }()
	if _, err := Acquire(context.Background(), st, dest, sys, "test", Options{MaxBytes: 64 << 20}); !errors.Is(err, ErrSpace) {
		t.Fatalf("space failure %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("space failure published")
	}
}

func TestAcquireEmptyHasNoReferenceClock(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dest := filepath.Join(privateRoot(t), "empty")
	m, err := Acquire(context.Background(), st, dest, System{ID: "shop-empty", CollectionIntervalNS: int64(time.Minute)}, "test", Options{MaxBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if m.Boundaries.DataReference != nil || m.Boundaries.LatestEpoch != nil || m.Boundaries.FirstCollectionAt != nil {
		t.Fatal("fabricated empty reference")
	}
	if m.System.RetentionNS != nil {
		t.Fatal("unknown retention fabricated")
	}
	d, err := Open(context.Background(), dest)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
}
func TestAcquireBusyAndMidflightSpaceCancellation(t *testing.T) {
	st, sys := synthetic(t)
	root := privateRoot(t)
	acquiring.Store(true)
	_, err := Acquire(context.Background(), st, filepath.Join(root, "busy"), sys, "test", Options{MaxBytes: 64 << 20})
	acquiring.Store(false)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("busy %v", err)
	}
	old := availableBytes
	var calls atomic.Int64
	availableBytes = func(string) (int64, error) {
		if calls.Add(1) == 1 {
			return 1 << 40, nil
		}
		return 0, nil
	}
	defer func() { availableBytes = old }()
	_, err = Acquire(context.Background(), st, filepath.Join(root, "cancelled"), sys, "test", Options{MaxBytes: 64 << 20})
	if !errors.Is(err, ErrSpace) {
		t.Fatalf("midflight disk failure %v (calls %d)", err, calls.Load())
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatalf("failure published files: %v %v", entries, e)
	}
	if _, err = st.DB().Exec("SELECT count(*) FROM epochs"); err != nil {
		t.Fatal(err)
	}
}
func TestAcquireMidflightContextCancellation(t *testing.T) {
	st, sys := synthetic(t)
	root := privateRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := availableBytes
	var calls atomic.Int64
	availableBytes = func(string) (int64, error) {
		if calls.Add(1) > 1 {
			cancel()
		}
		return 1 << 40, nil
	}
	defer func() { availableBytes = old }()
	_, err := Acquire(ctx, st, filepath.Join(root, "cancelled"), sys, "test", Options{MaxBytes: 64 << 20})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatalf("cancel published files: %v %v", entries, e)
	}
	snap := collect.NewSnapshot(time.Now().UTC())
	snap.FinishedAt = snap.StartedAt.Add(time.Second)
	if _, err = st.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
}

func TestInspectDatabaseBorrowedHandleAndEmptyGuards(t *testing.T) {
	if _, err := InspectDatabase(context.Background(), nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectDatabase(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	path, m := acquireFixture(t)
	db, err := OpenDatabase(context.Background(), filepath.Join(path, DataFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err := InspectDatabase(context.Background(), db)
	if err != nil || !reflect.DeepEqual(b, m.Boundaries) {
		t.Fatalf("inspection %+v %v", b, err)
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM epochs").Scan(&n); err != nil || n != 1 {
		t.Fatal("borrowed database closed")
	}
}
func TestOpenRejectsUnsupportedMetadataAndMissingSources(t *testing.T) {
	if db, err := OpenDatabase(context.Background(), filepath.Join(t.TempDir(), "missing.duckdb")); err == nil {
		db.Close()
		t.Fatal("missing source opened")
	}
	if _, err := Open(context.Background(), t.TempDir()); err == nil {
		t.Fatal("incomplete source opened")
	}
	path, m := acquireFixture(t)
	for _, which := range []string{"engine", "schema", "scope", "interval", "size", "hash", "configuration", "malformed", "oversize", "trailing"} {
		t.Run(which, func(t *testing.T) {
			changed := m
			switch which {
			case "engine":
				changed.EngineVersion = "v0.0.0"
			case "schema":
				changed.SchemaVersion++
			case "scope":
				changed.Scope = "filtered"
			case "interval":
				changed.System.CollectionIntervalNS = 0
			case "size":
				changed.Data.Size = -1
			case "hash":
				changed.Data.SHA256 = strings.Repeat("z", 64)
			case "configuration":
				changed.ChecksConfiguration = "available"
			}
			b, _ := json.Marshal(changed)
			switch which {
			case "malformed":
				b = []byte("{")
			case "oversize":
				b = []byte(strings.Repeat(" ", MaxManifestBytes+1))
			case "trailing":
				b = append(b, []byte(" {}")...)
			}
			if err := os.WriteFile(filepath.Join(path, ManifestFile), b, 0600); err != nil {
				t.Fatal(err)
			}
			if d, err := Open(context.Background(), path); err == nil {
				d.Close()
				t.Fatal("unsupported metadata accepted")
			}
		})
	}
}
func TestOpenDatabaseChecksumMismatchNeverRepairs(t *testing.T) {
	path, _ := acquireFixture(t)
	p := filepath.Join(path, DataFile)
	c, err := duckdb.NewConnector(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(c)
	if _, err = db.Exec("UPDATE migrations SET sha256='synthetic_mismatch' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if db, err := OpenDatabase(context.Background(), p); !errors.Is(err, ErrVersion) {
		if db != nil {
			db.Close()
		}
		t.Fatalf("mismatch %v", err)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("read-only validation mutated or repaired source")
	}
}

func TestInvalidLatestCollectionHasNoReferenceClock(t *testing.T) {
	st, sys := synthetic(t)
	// Only synthetic malformed publication: completion before collection start.
	snap := collect.NewSnapshot(time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC))
	snap.FinishedAt = snap.StartedAt.Add(-time.Second)
	if _, err := st.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(privateRoot(t), "invalid-clock")
	m, err := Acquire(context.Background(), st, dest, sys, "test", Options{MaxBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if m.Boundaries.DataReference != nil {
		t.Fatal("invalid completion supplied a clock")
	}
}

func TestFingerprintDatabaseCanonicalAndBounded(t *testing.T) {
	path, m := acquireFixture(t)
	p := filepath.Join(path, DataFile)
	db, err := OpenDatabase(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sum, err := FingerprintDatabase(context.Background(), p)
	if err != nil || sum != m.Data.SHA256 {
		t.Fatalf("fingerprint %s %v", sum, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = FingerprintDatabase(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := FingerprintDatabase(context.Background(), p); !errors.Is(err, ErrBudget) {
			t.Fatal(err)
		}
	}
	sparse := filepath.Join(t.TempDir(), "oversize")
	f, err := os.Create(sparse)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxDataBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err = FingerprintDatabase(context.Background(), sparse); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
}
func TestOpenDatabasePreservesCancellation(t *testing.T) {
	path, _ := acquireFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if db, err := OpenDatabase(ctx, filepath.Join(path, DataFile)); !errors.Is(err, context.Canceled) {
		if db != nil {
			db.Close()
		}
		t.Fatalf("cancel reclassified as %v", err)
	}
}

func TestAtomicPublicationNeverReplacesExistingDirectory(t *testing.T) {
	root := privateRoot(t)
	from := filepath.Join(root, "partial")
	to := filepath.Join(root, "good")
	if err := os.Mkdir(from, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(to, 0700); err != nil {
		t.Fatal(err)
	}
	if err := renameNew(from, to); !errors.Is(err, os.ErrExist) {
		t.Fatalf("no-replace rename returned %v", err)
	}
	if _, err := os.Stat(from); err != nil {
		t.Fatal("staging unexpectedly removed")
	}
	if _, err := os.Stat(to); err != nil {
		t.Fatal("existing artifact unexpectedly removed")
	}
}

func TestManifestNewlineCountsAgainstArtifactBudget(t *testing.T) {
	root := privateRoot(t)
	m := Manifest{Data: Data{Size: 10}}
	first := filepath.Join(root, "complete.json")
	if err := writeManifest(first, m, 1<<20); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "must-not-publish.json")
	if err = writeManifest(dest, m, m.Data.Size+info.Size()-1); !errors.Is(err, ErrBudget) {
		t.Fatalf("budget omitted trailing newline: %v", err)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("oversize manifest created")
	}
}

func TestAcquireDuringActivePublication(t *testing.T) {
	st, sys := synthetic(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	runs := []store.CheckRun{{Code: "stream-001", EntityType: "stream", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "APP/jobs", Severity: "warning", Detail: "Synthetic finding"}}}}
	if _, err := st.ApplyCheckRuns(ctx, 1, runs); err != nil {
		t.Fatal(err)
	}
	mute, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-001", Reason: "Synthetic review", MutedBy: "operator", CreatedAt: at, ExpiresAt: at.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	ready := make(chan struct{})
	var n atomic.Int64
	go func() {
		for i := int64(2); ; i++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			snap := collect.NewSnapshot(at.Add(time.Duration(i) * time.Minute))
			snap.FinishedAt = snap.StartedAt.Add(time.Second)
			epoch, err := st.WriteSnapshot(ctx, snap)
			if err != nil {
				done <- err
				return
			}
			if err = st.WriteEvents(ctx, []events.Event{{Time: snap.FinishedAt, ReceivedAt: snap.FinishedAt, Source: events.SourceCollector, Kind: "collector.gap", Summary: "Synthetic active event"}}); err != nil {
				done <- err
				return
			}
			if _, err = st.ApplyCheckRuns(ctx, epoch, runs); err != nil {
				done <- err
				return
			}
			if i == 2 {
				if _, err = st.UnmuteMute(ctx, mute.ID, "operator", at.Add(time.Minute)); err != nil {
					done <- err
					return
				}
			}
			if _, err = st.Acknowledge(ctx, store.Ack{FindingID: store.FindingID("stream-001", "APP/jobs"), AckedBy: "operator", CreatedAt: snap.FinishedAt}); err != nil {
				done <- err
				return
			}
			n.Add(1)
			if i == 2 {
				close(ready)
			}
		}
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("active writer failed %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("active writer did not start")
	}
	dest := filepath.Join(privateRoot(t), "active")
	m, err := Acquire(ctx, st, dest, sys, "test", Options{MaxBytes: 64 << 20})
	close(stop)
	writerErr := <-done
	if writerErr != nil {
		t.Fatal(writerErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() < 2 {
		t.Fatal("publication did not progress during acquisition")
	}
	d, err := Open(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var latest int64
	if err = d.DB.QueryRow("SELECT max(epoch) FROM epochs").Scan(&latest); err != nil {
		t.Fatal(err)
	}
	if m.Boundaries.LatestEpoch == nil || latest != *m.Boundaries.LatestEpoch {
		t.Fatal("manifest and rows came from different publications")
	}
	if len(m.PendingStages) == 0 || m.ChecksConfiguration != "not_exported" {
		t.Fatal("pipeline state invented")
	}
}
