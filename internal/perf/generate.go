package perf

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// GenerateResult describes a generated store.
type GenerateResult struct {
	Profile Profile
	Path    string
	Epochs  int
	// TemplateWrites are the durations of the epochs written through
	// Store.WriteSnapshot, the collector's own path.
	TemplateWrites []time.Duration
	Elapsed        time.Duration
	Bytes          int64
	Rows           map[string]int64
}

// linear lists the columns of a table whose synthetic values grow in
// proportion to the epoch number, and the timestamp columns that move
// with the epoch's start. Every other column of a table with an epoch
// column is copied as it is.
var linear = map[string]struct{ counters, times []string }{
	"server_stats":   {[]string{"in_msgs", "in_bytes", "out_msgs", "out_bytes"}, nil},
	"connections":    {[]string{"in_msgs", "out_msgs", "in_bytes", "out_bytes"}, []string{"last_activity"}},
	"account_stats":  {[]string{"sent_msgs", "sent_bytes", "received_msgs", "received_bytes"}, nil},
	"routes":         {[]string{"in_msgs", "out_msgs", "in_bytes", "out_bytes"}, []string{"last_activity"}},
	"stream_stats":   {[]string{"messages", "bytes", "first_seq", "last_seq"}, []string{"first_ts", "last_ts"}},
	"consumer_stats": {[]string{"delivered_consumer_seq", "delivered_stream_seq", "ack_floor_consumer_seq", "ack_floor_stream_seq"}, []string{"delivered_last_active", "ack_floor_last_active", "reported_at"}},
}

// chunkEpochs is how many epochs one copy statement writes, so that rows
// stay in epoch order as the collector would write them.
const chunkEpochs = 120

// Generate writes a store of the profile at path, which must not exist:
// the first two epochs through Store.WriteSnapshot, the rest copied from
// the second in SQL with counters and times advanced, then events over the
// whole history and one check run on the newest epoch.
func Generate(ctx context.Context, p Profile, path string, log *slog.Logger) (GenerateResult, error) {
	res := GenerateResult{Profile: p, Path: path, Epochs: p.Epochs(), Rows: map[string]int64{}}
	if _, err := os.Stat(path); err == nil {
		return res, fmt.Errorf("%s exists; generate into a new path", path)
	}
	if p.Epochs() < 3 {
		return res, errors.New("history must hold at least three epochs")
	}
	begin := time.Now()
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		return res, err
	}
	defer st.Close()
	first := p.FirstEpochAt()
	for n := 1; n <= 2; n++ {
		t := time.Now()
		if _, err := st.WriteSnapshot(ctx, p.Snapshot(n, first.Add(time.Duration(n-1)*p.Interval))); err != nil {
			return res, fmt.Errorf("template epoch %d: %w", n, err)
		}
		res.TemplateWrites = append(res.TemplateWrites, time.Since(t))
	}
	log.Info("template epochs written", "writes", res.TemplateWrites)
	db := st.DB()
	if err := copyHistory(ctx, db, p, log); err != nil {
		return res, err
	}
	if err := writeEvents(ctx, db, p); err != nil {
		return res, err
	}
	set, err := checks.Load(checksConfig())
	if err != nil {
		return res, err
	}
	eng := &checks.Engine{Set: set, Log: log, Interval: p.Interval, Budget: 10 * time.Minute}
	t := time.Now()
	if _, err := eng.Run(ctx, st, int64(p.Epochs())); err != nil {
		return res, fmt.Errorf("checks: %w", err)
	}
	log.Info("checks run on the newest epoch", "took", time.Since(t))
	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		return res, err
	}
	if res.Rows, err = tableRows(ctx, db); err != nil {
		return res, err
	}
	if err := st.Close(); err != nil {
		return res, err
	}
	res.Elapsed = time.Since(begin)
	fi, err := os.Stat(path)
	if err != nil {
		return res, err
	}
	res.Bytes = fi.Size()
	return res, nil
}

// copyHistory copies epoch 2's rows to epochs 3..N, in chunks of
// chunkEpochs, writes the epochs rows, and extends the identity tables'
// last epoch to N.
func copyHistory(ctx context.Context, db *sql.DB, p Profile, log *slog.Logger) error {
	tables, err := epochTables(ctx, db)
	if err != nil {
		return err
	}
	n := p.Epochs()
	exec := func(q string, args ...any) error {
		_, err := db.ExecContext(ctx, q, args...)
		return err
	}
	firstMicros := p.FirstEpochAt().UnixMicro()
	step := p.Interval.Microseconds()
	for lo := 3; lo <= n; lo += chunkEpochs {
		hi := min(lo+chunkEpochs-1, n)
		if err := exec(`INSERT INTO epochs SELECT t.* REPLACE (i AS epoch,
  make_timestamp(?::BIGINT + (i - 1) * ?::BIGINT) AS started_at,
  make_timestamp(?::BIGINT + (i - 1) * ?::BIGINT + 400000) AS finished_at)
FROM epochs t, range(?, ? + 1) r(i) WHERE t.epoch = 2 ORDER BY i`, firstMicros, step, firstMicros, step, lo, hi); err != nil {
			return fmt.Errorf("epochs %d-%d: %w", lo, hi, err)
		}
		for _, t := range tables {
			repl := []string{"i AS epoch"}
			for _, c := range linear[t].counters {
				repl = append(repl, fmt.Sprintf("CAST(%s * i / 2 AS BIGINT) AS %s", c, c))
			}
			for _, c := range linear[t].times {
				repl = append(repl, fmt.Sprintf("%s + to_microseconds((i - 2) * %d) AS %s", c, step, c))
			}
			q := fmt.Sprintf(`INSERT INTO %s SELECT t.* REPLACE (%s) FROM %s t, range(?, ? + 1) r(i) WHERE t.epoch = 2 ORDER BY i`,
				t, strings.Join(repl, ", "), t)
			if err := exec(q, lo, hi); err != nil {
				return fmt.Errorf("%s epochs %d-%d: %w", t, lo, hi, err)
			}
		}
		if (lo/chunkEpochs)%10 == 0 {
			log.Info("history copied", "through_epoch", hi, "of", n)
		}
	}
	if err := exec(`SELECT setval('epoch_seq', ?)`, n); err != nil {
		return err
	}
	for _, t := range []string{"servers", "streams", "consumers", "accounts", "stream_config", "consumer_config", "raft_groups"} {
		if err := exec(fmt.Sprintf(`UPDATE %s SET last_epoch = ? WHERE last_epoch = 2`, t), n); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	return exec(`UPDATE servers SET last_seen_at = ?`, LastEpochAt)
}

// epochTables are the base tables with an epoch column whose rows belong
// to one epoch's snapshot; epochs itself and the checks' and
// notifications' tables are left out.
func epochTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.table_name FROM information_schema.columns c
JOIN information_schema.tables t USING (table_name)
WHERE c.column_name = 'epoch' AND t.table_type = 'BASE TABLE'
  AND c.table_name NOT IN ('epochs', 'check_runs', 'finding_transitions', 'notify_outbox', 'notify_state')
ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// writeEvents stores EventsPerHour events in every hour of the history:
// client connects and disconnects, consumer creates and deletes and leader
// elections, spread evenly in time.
func writeEvents(ctx context.Context, db *sql.DB, p Profile) error {
	hours := int(p.History / time.Hour)
	total := max(hours, 1) * p.EventsPerHour
	if total == 0 {
		return nil
	}
	spacing := p.History.Microseconds() / int64(total)
	_, err := db.ExecContext(ctx, `INSERT INTO events (id, time, received_at, source, type, kind, server_name, account, stream,
  consumer, client_name, client_host, reason, summary, raw)
SELECT nextval('event_seq'), make_timestamp(?::BIGINT + i * ?::BIGINT), make_timestamp(?::BIGINT + i * ?::BIGINT + 2000),
  CASE WHEN i % 5 < 2 THEN 'sys' ELSE 'jetstream' END,
  'perf',
  ['client.connect', 'client.disconnect', 'consumer.created', 'consumer.deleted', 'stream.leader_elected'][i % 5 + 1],
  'nats-' || (i % ?), 'APERFACCOUNT' || lpad(CAST(i % ? AS VARCHAR), 3, '0'),
  CASE WHEN i % 5 >= 2 THEN 'order-jobs-' || lpad(CAST(i % ? AS VARCHAR), 4, '0') END,
  CASE WHEN i % 5 IN (2, 3) THEN 'resize-worker-' || lpad(CAST(i % ? AS VARCHAR), 4, '0') || '-0' END,
  CASE WHEN i % 5 < 2 THEN 'media-worker-' || (i % 400) END,
  CASE WHEN i % 5 < 2 THEN '10.1.' || (i % 250) || '.' || (i % 249) END,
  CASE WHEN i % 5 = 1 THEN 'Client Closed' END,
  'synthetic event ' || i, '{"perf":true}'::JSON
FROM range(?) r(i) ORDER BY i`,
		p.FirstEpochAt().UnixMicro(), spacing, p.FirstEpochAt().UnixMicro(), spacing,
		p.Servers, p.Accounts, p.Streams, p.Streams, total)
	return err
}

// tableRows counts the rows of every base table.
func tableRows(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM duckdb_tables() WHERE schema_name = 'main' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, t)
	}
	rows.Close()
	out := map[string]int64{}
	for _, t := range names {
		var n int64
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, t)).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			out[t] = n
		}
	}
	return out, nil
}

// checksConfig is the checks' configuration of a default natslens run.
func checksConfig() config.Checks { return config.Default().Checks }
