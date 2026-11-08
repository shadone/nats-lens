package dataset

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

const MaxManifestBytes = 64 << 10
const MaxDataBytes int64 = 4 << 30

// OpenDatabase opens a closed trusted bare database read-only, verifies the exact
// schema and engine, and transfers Close ownership to the caller. It never opens
// an active collector's store, replays a WAL, migrates, repairs, or checkpoints.
func OpenDatabase(ctx context.Context, path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrInvalid
	}
	if strings.ContainsAny(path, "?#%") {
		return nil, ErrInvalid
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, ErrInvalid
	}
	if _, err = os.Lstat(path + ".wal"); !os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: WAL present or inaccessible", ErrInvalid)
	}
	db, err := store.ReadOnlyDB(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%w: closed database cannot open read-only or schema differs", ErrVersion)
	}
	return db, nil
}

// Open verifies the manifest, private regular files, SHA256, schema, engine and
// provenance before returning the database. Only trusted datasets are supported.
func Open(ctx context.Context, path string) (*Dataset, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrInvalid
	}
	st, err := os.Lstat(path)
	if err != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: dataset directory must be private and not a symlink", ErrInvalid)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrInvalid
	}
	defer root.Close()
	entries, err := root.FS().(interface {
		ReadDir(string) ([]os.DirEntry, error)
	}).ReadDir(".")
	if err != nil {
		return nil, ErrInvalid
	}
	if len(entries) != 2 {
		return nil, fmt.Errorf("%w: unexpected or partial dataset files", ErrInvalid)
	}
	for _, e := range entries {
		if e.Name() != ManifestFile && e.Name() != DataFile {
			return nil, ErrInvalid
		}
		i, e := root.Lstat(e.Name())
		if e != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
			return nil, ErrInvalid
		}
	}
	f, err := root.Open(ManifestFile)
	if err != nil {
		return nil, ErrInvalid
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxManifestBytes+1))
	if err != nil || len(b) > MaxManifestBytes {
		return nil, ErrInvalid
	}
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&m); err != nil {
		return nil, ErrInvalid
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	if m.FormatVersion != FormatVersion {
		return nil, ErrVersion
	}
	if !m.Complete || m.Scope != FullSystemScope || m.Data.Path != DataFile || m.Data.Size <= 0 || m.Data.Size > MaxDataBytes || len(m.Data.SHA256) != 64 || m.System.ID == "" || m.System.CollectionIntervalNS <= 0 || m.CaptureStartedAt.IsZero() || m.ExportedAt.Before(m.CaptureStartedAt) || m.ChecksConfiguration != "not_exported" {
		return nil, ErrInvalid
	}
	if _, err = hex.DecodeString(m.Data.SHA256); err != nil {
		return nil, ErrInvalid
	}
	data, err := root.Open(DataFile)
	if err != nil {
		return nil, ErrInvalid
	}
	sum, size, err := digest(ctx, data)
	data.Close()
	if err != nil {
		return nil, err
	}
	if sum != m.Data.SHA256 || size != m.Data.Size {
		return nil, ErrIntegrity
	}
	dbPath := filepath.Join(path, DataFile)
	db, err := OpenDatabase(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Dataset, error) { db.Close(); return nil, err }
	var engine string
	var schema int
	if err = db.QueryRowContext(ctx, "SELECT version(),(SELECT max(version) FROM migrations)").Scan(&engine, &schema); err != nil {
		return fail(ErrIntegrity)
	}
	if engine != m.EngineVersion || schema != m.SchemaVersion {
		return fail(ErrVersion)
	}
	bounds, err := readBoundaries(ctx, db)
	if err != nil {
		return fail(ErrIntegrity)
	}
	if !reflect.DeepEqual(bounds, m.Boundaries) {
		return fail(fmt.Errorf("%w: provenance differs from database", ErrIntegrity))
	}
	// Force a scan of every stored column, so corrupt pages fail before publication.
	if err = scanTables(ctx, db); err != nil {
		return fail(fmt.Errorf("%w: table scan failed", ErrIntegrity))
	}
	return &Dataset{Manifest: m, DB: db, DBPath: dbPath}, nil
}

func digest(ctx context.Context, r io.Reader) (string, int64, error) {
	h := sha256.New()
	buf := make([]byte, 128<<10)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return "", n, err
		}
		nr, err := r.Read(buf)
		if nr > 0 {
			h.Write(buf[:nr])
			n += int64(nr)
		}
		if err == io.EOF {
			return hex.EncodeToString(h.Sum(nil)), n, nil
		}
		if err != nil {
			return "", n, err
		}
	}
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readBoundaries(ctx context.Context, q queryer) (Boundaries, error) {
	var b Boundaries
	err := q.QueryRowContext(ctx, `SELECT min(epoch),max(epoch),min(started_at),arg_max(started_at,epoch),arg_max(finished_at,epoch) FROM epochs`).Scan(&b.FirstEpoch, &b.LatestEpoch, &b.FirstCollectionAt, &b.LastCollectionStartedAt, &b.DataReference)
	if err != nil {
		return b, err
	}
	err = q.QueryRowContext(ctx, `SELECT min(time),max(time),max(received_at),max(id) FROM events`).Scan(&b.FirstEventAt, &b.LastEventAt, &b.LastEventReceivedAt, &b.LatestEventID)
	if err != nil {
		return b, err
	}
	err = q.QueryRowContext(ctx, `SELECT count(*),max(epoch),(SELECT count(*) FROM epochs e WHERE NOT EXISTS (SELECT 1 FROM check_runs c WHERE c.epoch=e.epoch)) FROM check_runs`).Scan(&b.CheckRuns, &b.LatestEvaluatedEpoch, &b.PendingEvaluationEpochs)
	for _, t := range []*time.Time{b.FirstCollectionAt, b.LastCollectionStartedAt, b.DataReference, b.FirstEventAt, b.LastEventAt, b.LastEventReceivedAt} {
		if t != nil {
			*t = t.UTC()
		}
	}
	return b, err
}

func scanTables(ctx context.Context, q queryer) error {
	rows, err := q.QueryContext(ctx, "SELECT table_name FROM duckdb_tables() WHERE NOT internal AND schema_name='main' ORDER BY table_name")
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		names = append(names, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, n := range names {
		var hash any
		if err = q.QueryRowContext(ctx, "SELECT bit_xor(hash(t)) FROM "+quoteIdentifier(n)+" t").Scan(&hash); err != nil {
			return err
		}
	}
	return nil
}
func quoteIdentifier(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
