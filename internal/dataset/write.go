package dataset

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

const MaxDuration = 5 * time.Minute
const DefaultMinFreeBytes int64 = 256 << 20

var acquiring atomic.Bool
var availableBytes = freeBytes
var safeVersion = regexp.MustCompile(`^[A-Za-z0-9._+\-]{1,128}$`)

type Options struct {
	// MaxBytes bounds successful output; a monitor interrupts native work that
	// crosses it. Native writes can overshoot before interruption is observed.
	MaxBytes     int64
	MinFreeBytes int64
}

// Acquire publishes one new, full per-system dataset from an open Store. The
// collector's own engine performs a logical copy; source files are never copied,
// checkpointed, repaired, or migrated. Only one acquisition runs per process.
func Acquire(ctx context.Context, st *store.Store, destination string, system System, applicationVersion string, o Options) (Manifest, error) {
	var m Manifest
	if !acquiring.CompareAndSwap(false, true) {
		return m, ErrBusy
	}
	defer acquiring.Store(false)
	ctx, cancel := context.WithTimeout(ctx, MaxDuration)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return m, err
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = MaxDataBytes
	}
	if o.MinFreeBytes == 0 {
		o.MinFreeBytes = DefaultMinFreeBytes
	}
	if o.MaxBytes < 1 || o.MaxBytes > MaxDataBytes || o.MinFreeBytes < 0 || o.MinFreeBytes > MaxDataBytes || system.ID == "" || len(system.ID) > 128 || len(system.Name) > 512 || system.CollectionIntervalNS <= 0 || !safeVersion.MatchString(applicationVersion) {
		return m, ErrInvalid
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return m, ErrInvalid
	}
	parent := filepath.Dir(destination)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return m, fmt.Errorf("%w: parent must be an existing private directory", ErrInvalid)
	}
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return m, ErrInvalid
	}
	parent = canonical
	destination = filepath.Join(parent, filepath.Base(destination))
	if _, err = os.Lstat(destination); !os.IsNotExist(err) {
		return m, ErrExists
	}
	free, err := availableBytes(parent)
	if err != nil {
		return m, err
	}
	if free < o.MaxBytes+o.MinFreeBytes {
		return m, ErrSpace
	}
	stage, err := os.MkdirTemp(parent, ".dataset-partial-")
	if err != nil {
		return m, err
	}
	// Remove only the directory we created. A crash leaves this private,
	// explicitly incomplete directory for an operator to inspect/remove.
	defer os.RemoveAll(stage)
	work, cancelWork := context.WithCancelCause(ctx)
	defer cancelWork(nil)
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-work.Done():
				return
			case <-tick.C:
				var size int64
				e := filepath.WalkDir(stage, func(p string, d fs.DirEntry, e error) error {
					if e != nil {
						return e
					}
					if !d.IsDir() {
						i, e := d.Info()
						if e != nil {
							return e
						}
						size += i.Size()
					}
					return nil
				})
				if e != nil {
					cancelWork(e)
					return
				}
				if size > o.MaxBytes {
					cancelWork(ErrBudget)
					return
				}
				available, e := availableBytes(parent)
				if e != nil {
					cancelWork(e)
					return
				}
				if available < o.MinFreeBytes {
					cancelWork(ErrSpace)
					return
				}
			}
		}
	}()
	defer func() { close(stop); <-stopped }()
	m = Manifest{FormatVersion: FormatVersion, Scope: FullSystemScope, System: system, ApplicationVersion: applicationVersion, CaptureStartedAt: time.Now().UTC(), Complete: true, ChecksConfiguration: "not_exported",
		PendingStages: []string{"collection_and_event_buffers_unknown", "evaluation_pipeline_completion_unknown"},
		ExportedFacts: []string{"all_retained_database_tables_and_catalog", "findings_and_check_runs_as_published", "durable_mutes_and_acknowledgements", "inventory_and_durable_notification_state", "archive_watermarks_without_archive_files"},
		LiveOnly:      []string{"credentials_and_runtime_configuration", "connections_and_readiness", "collection_event_and_notification_callbacks", "hourly_archive_files", "in_memory_buffers_and_in_flight_evaluations"}}
	dataPath := filepath.Join(stage, DataFile)
	err = st.SnapshotCopy(work, dataPath, func(tx *sql.Tx) error {
		var err error
		m.Boundaries, err = readBoundaries(work, tx)
		if err != nil {
			return err
		}
		return tx.QueryRowContext(work, "SELECT version(),(SELECT max(version) FROM migrations)").Scan(&m.EngineVersion, &m.SchemaVersion)
	})
	if err != nil {
		return Manifest{}, acquisitionError(work, err)
	}
	if err = os.Chmod(dataPath, 0600); err != nil {
		return Manifest{}, err
	}
	f, err := os.Open(dataPath)
	if err != nil {
		return Manifest{}, err
	}
	sum, size, err := digest(work, f)
	f.Close()
	if err != nil {
		return Manifest{}, acquisitionError(work, err)
	}
	if size > o.MaxBytes {
		return Manifest{}, ErrBudget
	}
	m.Data = Data{Path: DataFile, Size: size, SHA256: sum}
	m.ExportedAt = time.Now().UTC()
	if err = writeManifest(filepath.Join(stage, ManifestFile), m, o.MaxBytes); err != nil {
		return Manifest{}, err
	}
	// Opening rechecks hashes/provenance, exact schema checksums, engine and every
	// stored column, using the same reader as offline consumers.
	d, err := Open(work, stage)
	if err != nil {
		return Manifest{}, acquisitionError(work, err)
	}
	if err = d.Close(); err != nil {
		return Manifest{}, err
	}
	if err = syncFile(dataPath); err != nil {
		return Manifest{}, err
	}
	if err = syncFile(stage); err != nil {
		return Manifest{}, err
	}
	if err = work.Err(); err != nil {
		return Manifest{}, acquisitionError(work, err)
	}
	if err = renameNew(stage, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Manifest{}, ErrExists
		}
		return Manifest{}, err
	}
	if err = syncFile(parent); err != nil {
		return m, fmt.Errorf("dataset published but directory sync failed: %w", err)
	}
	return m, nil
}
func acquisitionError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return store.SafeError("dataset acquisition", err)
}
func writePrivateSynced(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if e := f.Sync(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}
func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func writeManifest(path string, m Manifest, maxBytes int64) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if m.Data.Size+int64(len(b)) > maxBytes {
		return ErrBudget
	}
	return writePrivateSynced(path, b)
}
