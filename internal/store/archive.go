package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// archiveTable says how to pick a table's rows for one hour and how to
// merge its files on a rebuild.
type archiveTable struct {
	name string
	// where selects the hour's rows; %[1]s and %[2]s are the hour's start
	// and end as TIMESTAMP literals. Empty exports the whole table every
	// hour.
	where string
	// key is the identity of a row in a table whose rows are updated after
	// they are written (identity and configuration tables). Such a row is
	// exported in every hour it was touched, and a rebuild keeps the copy
	// with the highest last_epoch. Nil for tables whose rows never change.
	key []string
	// latest orders the copies of one key, newest first; empty means
	// last_epoch DESC.
	latest string
	// columns lists columns latest reads that files written before a
	// migration lack, as "NULL::TYPE AS name"; the rebuild adds them,
	// NULL, to such files' rows.
	columns []string
}

const (
	hourEpochs = `epoch IN (SELECT epoch FROM epochs WHERE started_at >= %[1]s AND started_at < %[2]s)`
	// A range row is touched in an hour when its epoch range overlaps the
	// hour's epochs.
	hourRange = `first_epoch <= (SELECT max(epoch) FROM epochs WHERE started_at >= %[1]s AND started_at < %[2]s)
  AND last_epoch >= (SELECT min(epoch) FROM epochs WHERE started_at >= %[1]s AND started_at < %[2]s)`
)

// archiveTables lists every archived table, in rebuild order.
func archiveTables() []archiveTable {
	out := []archiveTable{{name: "epochs", where: `started_at >= %[1]s AND started_at < %[2]s`}}
	for _, t := range append(append(append([]string{}, epochTables...), accountEpochTables...), checkTables...) {
		out = append(out, archiveTable{name: t, where: hourEpochs})
	}
	keys := map[string][]string{
		"servers":         {"server_id"},
		"accounts":        {"account"},
		"streams":         {"account", "stream"},
		"consumers":       {"account", "stream", "consumer"},
		"raft_groups":     {"group_id"},
		"stream_config":   {"server_id", "account", "stream", "first_epoch"},
		"consumer_config": {"server_id", "account", "stream", "consumer", "first_epoch"},
	}
	for _, t := range rangeTables {
		out = append(out, archiveTable{name: t, where: hourRange, key: keys[t]})
	}
	// The hourly sums of micro service endpoints change only while their
	// hour's epochs are written.
	out = append(out, archiveTable{name: "micro_endpoint_hours", where: `hour >= %[1]s AND hour < %[2]s`})
	// A Kubernetes Event's row moves to the hour of its latest occurrence;
	// when an earlier hour's files still hold it, the rebuild keeps the
	// copy received last.
	out = append(out, archiveTable{name: "events", where: `received_at >= %[1]s AND received_at < %[2]s`,
		key: []string{"id"}, latest: "received_at DESC"})
	// Mutes are operator decisions, not observations: every hour holds the
	// whole table as it was at export, and a rebuild keeps each mute's
	// newest copy, which is the lifted one once it was lifted. Mutes are
	// never swept.
	// Among copies that are not lifted, the one with the acceptance's
	// growth baseline wins: the review writes it later, and unmuted_at
	// alone ties.
	out = append(out, archiveTable{name: "mutes", key: []string{"id"}, latest: "unmuted_at DESC NULLS LAST, growth_per_day IS NULL",
		columns: []string{"NULL::DOUBLE AS growth_per_day"}})
	// Findings are the current state of every check, updated after every
	// epoch: like mutes, every hour holds the whole table, and a rebuild
	// keeps each finding's copy with the highest updated_epoch. Their
	// transitions and the check runs are archived by epoch above.
	out = append(out, archiveTable{name: "findings", key: []string{"id"}, latest: "updated_epoch DESC"})
	return out
}

// ArchiveTableNames returns the names of the archived tables.
func ArchiveTableNames() []string {
	var out []string
	for _, t := range archiveTables() {
		out = append(out, t.name)
	}
	return out
}

func tsLiteral(t time.Time) string {
	return "TIMESTAMP '" + t.UTC().Format("2006-01-02 15:04:05") + "'"
}

func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// PendingHour is an hour the exporter has to write.
type PendingHour struct {
	Hour time.Time
	// Final is false for an hour that has not closed yet: it is exported
	// provisionally and again once closed.
	Final bool
}

// PendingHours returns the hours from from on that hold epochs or events
// and need an export, oldest first. Hours before closedBefore are closed:
// they are pending when never exported, exported only provisionally, or
// when their epoch or event count differs from the exported one (a late
// event or epoch). The newest hour also counts as changed when a mute was
// created or lifted since its export: every hour holds the whole mutes
// table, and a rebuild loads the newest hour, so only that one has to carry
// the current mutes. Later hours are open: they are pending provisionally
// when provisionalEvery is positive, their counts changed, and their last
// export is at least provisionalEvery before now. All bounds are hour
// starts except now.
func (s *Store) PendingHours(ctx context.Context, from, closedBefore, now time.Time, provisionalEvery time.Duration) ([]PendingHour, error) {
	rows, err := s.db.QueryContext(ctx, `
WITH counts AS (
    SELECT h, sum(epochs) AS epochs, sum(events) AS events FROM (
        SELECT date_trunc('hour', started_at) AS h, count(*) AS epochs, 0 AS events
        FROM epochs WHERE started_at >= ? GROUP BY 1
        UNION ALL
        SELECT date_trunc('hour', received_at), 0, count(*)
        FROM events WHERE received_at >= ? GROUP BY 1
        UNION ALL
        -- An exported hour whose rows all moved away (a Kubernetes Event
        -- updated later) is exported again, empty.
        SELECT hour, 0, 0 FROM archive_hours WHERE hour >= ?
    ) GROUP BY h
)
SELECT c.h, c.h < ? AS closed
FROM counts c LEFT JOIN archive_hours a ON a.hour = c.h
WHERE a.hour IS NULL
   OR a.epochs IS DISTINCT FROM c.epochs OR a.events IS DISTINCT FROM c.events
   OR (c.h = (SELECT max(h) FROM counts) AND a.mute_changes IS DISTINCT FROM (`+muteChanges+`))
   OR (c.h < ? AND NOT coalesce(a.final, false))
ORDER BY c.h`, from.UTC(), from.UTC(), from.UTC(), closedBefore.UTC(), closedBefore.UTC())
	if err != nil {
		return nil, fmt.Errorf("pending hours: %w", err)
	}
	type cand struct {
		h      time.Time
		closed bool
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.h, &c.closed); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []PendingHour
	for _, c := range cands {
		if c.closed {
			out = append(out, PendingHour{Hour: c.h.UTC(), Final: true})
			continue
		}
		if provisionalEvery <= 0 {
			continue
		}
		last, err := s.lastExport(ctx, c.h)
		if err != nil {
			return nil, err
		}
		if last.IsZero() || !now.Before(last.Add(provisionalEvery)) {
			out = append(out, PendingHour{Hour: c.h.UTC()})
		}
	}
	return out, nil
}

// lastExport returns when the hour was last exported, zero when never.
func (s *Store) lastExport(ctx context.Context, hour time.Time) (time.Time, error) {
	var at time.Time
	err := s.db.QueryRowContext(ctx, `SELECT exported_at FROM archive_hours WHERE hour = ?`, hour.UTC()).Scan(&at)
	if isNoRows(err) {
		return time.Time{}, nil
	}
	return at.UTC(), err
}

// ExportedFile is one table's rows for one hour, written to Path.
type ExportedFile struct {
	Table string
	Path  string
	Rows  int64
}

// HourExport is what ExportHour wrote for one hour.
type HourExport struct {
	Files []ExportedFile
	// Epochs and Events are the hour's epoch and event counts, and
	// MuteChanges the mutes plus lifted mutes plus acceptances with a
	// growth, read in the same transaction as the files.
	Epochs, Events, MuteChanges int64
}

// muteChanges counts mutes, lifted mutes and acceptances with a growth.
// Mutes are never deleted and a growth is never cleared, so the number
// grows with every mute created or lifted and every baseline the review
// writes, and the hour of a baseline is exported again.
const muteChanges = `SELECT count(*) + count(unmuted_at) + count(growth_per_day) FROM mutes`

// Rows returns the rows written across all files.
func (e HourExport) Rows() int64 {
	var n int64
	for _, f := range e.Files {
		n += f.Rows
	}
	return n
}

// ExportHour writes each table's rows for the hour starting at hour to the
// Parquet file path(table) names, reading all tables in one transaction so
// that they agree. Tables without rows for the hour get no file. The paths
// must be in existing directories.
func (s *Store) ExportHour(ctx context.Context, hour time.Time, path func(table string) string) (HourExport, error) {
	var out HourExport
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	start, end := tsLiteral(hour), tsLiteral(hour.Add(time.Hour))
	for _, t := range archiveTables() {
		where := "true"
		if t.where != "" {
			where = fmt.Sprintf(t.where, start, end)
		}
		var n int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+t.name+" WHERE "+where).Scan(&n); err != nil {
			return out, fmt.Errorf("count %s: %w", t.name, err)
		}
		switch t.name {
		case "epochs":
			out.Epochs = n
		case "events":
			out.Events = n
		}
		if n == 0 {
			continue
		}
		p := path(t.name)
		q := fmt.Sprintf("COPY (SELECT * FROM %s WHERE %s) TO %s (FORMAT parquet, COMPRESSION zstd)", t.name, where, sqlString(p))
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return out, fmt.Errorf("export %s: %w", t.name, err)
		}
		out.Files = append(out.Files, ExportedFile{Table: t.name, Path: p, Rows: n})
	}
	if err := tx.QueryRowContext(ctx, muteChanges).Scan(&out.MuteChanges); err != nil {
		return out, fmt.Errorf("count mute changes: %w", err)
	}
	return out, tx.Commit()
}

// MarkExported records an exported hour with the counts it was exported
// with.
func (s *Store) MarkExported(ctx context.Context, hour time.Time, e HourExport, final bool, at time.Time) error {
	perTable := map[string]int64{}
	for _, f := range e.Files {
		perTable[f.Table] = f.Rows
	}
	tableRows, err := json.Marshal(perTable)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(ctx, `INSERT INTO archive_hours (hour, rows, files, exported_at, epochs, events, final, table_rows, mute_changes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (hour) DO UPDATE SET rows = excluded.rows, files = excluded.files, exported_at = excluded.exported_at,
    epochs = excluded.epochs, events = excluded.events, final = excluded.final, table_rows = excluded.table_rows,
    mute_changes = excluded.mute_changes`,
		hour.UTC(), e.Rows(), len(e.Files), at.UTC(), e.Epochs, e.Events, final, string(tableRows), e.MuteChanges)
	return err
}

// ParquetRows returns the row count the metadata of the Parquet file at
// path records.
func (s *Store) ParquetRows(ctx context.Context, path string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, "SELECT coalesce(sum(num_rows), 0) FROM parquet_file_metadata("+sqlString(path)+")").Scan(&n)
	return n, err
}

// MaxIDs are the highest numbers handed out by the sequences a rebuild
// restarts.
type MaxIDs struct {
	Epoch, Event, Mute int64
}

// MaxIDs returns the highest epoch number, event ID and mute ID stored, 0
// when none.
func (s *Store) MaxIDs(ctx context.Context) (MaxIDs, error) {
	var m MaxIDs
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT coalesce(max(epoch), 0) FROM epochs),
    (SELECT coalesce(max(id), 0) FROM events), (SELECT coalesce(max(id), 0) FROM mutes)`).
		Scan(&m.Epoch, &m.Event, &m.Mute)
	return m, err
}

// RebuildHour is one archived hour to load.
type RebuildHour struct {
	// Files maps a table to its file for the hour.
	Files map[string]string
	Final bool
}

// Rebuild loads archived Parquet files into an empty database in one
// transaction and records the hours as exported. Rows of identity and
// configuration tables appear once per hour they were touched; the copy
// with the highest last_epoch is kept; of a mute, the newest copy (the
// lifted one once it was lifted). The epoch, event and mute sequences
// continue after the highest loaded numbers, or after those in min when
// they are higher (the archive's record of numbers handed out after its
// last full hour).
//
// Rows are matched to columns by name: a column added by a later migration
// is null in rows loaded from files written before it existed.
func (s *Store) Rebuild(ctx context.Context, hours map[time.Time]RebuildHour, min MaxIDs, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM epochs) + (SELECT count(*) FROM events)`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("rebuild needs an empty database, this one has %d epochs and events", n)
	}
	files := map[string][]string{}
	for _, h := range hours {
		for table, p := range h.Files {
			files[table] = append(files[table], p)
		}
	}
	for _, t := range archiveTables() {
		paths := files[t.name]
		if len(paths) == 0 {
			continue
		}
		sort.Strings(paths)
		from := fmt.Sprintf("read_parquet([%s], union_by_name = true, hive_partitioning = false)", quoteAll(paths))
		if len(t.columns) > 0 {
			from = fmt.Sprintf("(SELECT * FROM %s UNION ALL BY NAME SELECT %s WHERE false)", from, strings.Join(t.columns, ", "))
		}
		q := fmt.Sprintf("INSERT INTO %s BY NAME SELECT * FROM %s", t.name, from)
		if t.key != nil {
			latest := t.latest
			if latest == "" {
				latest = "last_epoch DESC"
			}
			q += fmt.Sprintf(" QUALIFY row_number() OVER (PARTITION BY %s ORDER BY %s) = 1",
				strings.Join(t.key, ", "), latest)
		}
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("load %s: %w", t.name, err)
		}
	}
	if err := restartSequence(ctx, tx, "epoch_seq", fmt.Sprintf("SELECT greatest(coalesce(max(epoch), 0), %d) + 1 FROM epochs", min.Epoch)); err != nil {
		return err
	}
	if err := restartSequence(ctx, tx, "event_seq", fmt.Sprintf("SELECT greatest(coalesce(max(id), 0), %d) + 1 FROM events", min.Event)); err != nil {
		return err
	}
	if err := restartSequence(ctx, tx, "mute_seq", fmt.Sprintf("SELECT greatest(coalesce(max(id), 0), %d) + 1 FROM mutes", min.Mute)); err != nil {
		return err
	}
	for h, rh := range hours {
		var paths []string
		for _, p := range rh.Files {
			paths = append(paths, p)
		}
		// Times are literals: a parameter in a SELECT list binds as
		// TIMESTAMPTZ and would be shifted by the session time zone.
		start, end := tsLiteral(h), tsLiteral(h.Add(time.Hour))
		// An hour exported empty (its rows moved to a later hour) has no
		// files to read the row count from.
		from := fmt.Sprintf("parquet_file_metadata([%s])", quoteAll(paths))
		if len(paths) == 0 {
			from = "(SELECT 0 AS num_rows)"
		}
		q := fmt.Sprintf(`INSERT INTO archive_hours (hour, rows, files, exported_at, epochs, events, final)
SELECT %s, coalesce(sum(num_rows), 0), ?, %s,
    (SELECT count(*) FROM epochs WHERE started_at >= %s AND started_at < %s),
    (SELECT count(*) FROM events WHERE received_at >= %s AND received_at < %s), ?
FROM %s`, start, tsLiteral(at), start, end, start, end, from)
		if _, err := tx.ExecContext(ctx, q, len(paths), rh.Final); err != nil {
			return fmt.Errorf("mark %s: %w", h, err)
		}
	}
	return tx.Commit()
}

func quoteAll(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = sqlString(p)
	}
	return strings.Join(quoted, ", ")
}

// restartSequence recreates a sequence so that its next value is the
// result of the query next. Sequences cannot be altered in DuckDB, and no
// column default uses these, so they can be dropped.
func restartSequence(ctx context.Context, tx *sql.Tx, name, next string) error {
	var start int64
	if err := tx.QueryRowContext(ctx, next).Scan(&start); err != nil {
		return fmt.Errorf("restart %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP SEQUENCE %s; CREATE SEQUENCE %s START %d", name, name, start)); err != nil {
		return fmt.Errorf("restart %s: %w", name, err)
	}
	return nil
}
