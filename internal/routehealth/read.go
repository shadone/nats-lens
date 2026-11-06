package routehealth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Querier lets the check reuse its existing connection and timeout.
// ASVS 1.2.4: all collection and time inputs use query parameters.
type Querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ReadEarlier selects the newest route-bearing collection within three
// intervals. Endpoint failures and empty replies produce no invented pairs.
func ReadEarlier(ctx context.Context, db Querier, epoch int64, at time.Time, interval time.Duration) (*Earlier, error) {
	bound, known := EarliestEarlier(at, interval)
	if !known {
		return nil, nil
	}
	var e Earlier
	err := db.QueryRowContext(ctx, `SELECT e.epoch, e.started_at FROM epochs e
WHERE e.epoch < ? AND e.started_at >= ? AND e.started_at < ?
 AND EXISTS (SELECT 1 FROM routes r WHERE r.epoch = e.epoch)
ORDER BY e.epoch DESC LIMIT 1`, epoch, bound, at).Scan(&e.Epoch, &e.At)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("topology earlier collection: %w", err)
	}
	e.At = e.At.UTC()
	e.Routes, err = ReadRows(ctx, db, e.Epoch)
	return &e, err
}

func durPtr(v sql.NullInt64) *time.Duration {
	if !v.Valid {
		return nil
	}
	d := time.Duration(v.Int64)
	return &d
}
func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// ReadRows resolves peers as Topology does: captured name, then process
// name, then ID. It reads one whole collection, never once per server.
func ReadRows(ctx context.Context, db Querier, epoch int64) ([]Row, error) {
	rows, err := db.QueryContext(ctx, `
SELECT s.server_name, coalesce(s.cluster, ''), coalesce(nullif(r.remote_name, ''), rs.server_name, r.remote_id, ''),
  r.rtt_ns, r.pending_bytes, coalesce(r.account, '')
FROM routes r JOIN servers s USING (server_id) LEFT JOIN servers rs ON rs.server_id = r.remote_id
WHERE r.epoch = ?`, epoch)
	if err != nil {
		return nil, fmt.Errorf("topology routes: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var rtt, pending sql.NullInt64
		if err := rows.Scan(&r.Server, &r.Cluster, &r.Remote, &rtt, &pending, &r.Account); err != nil {
			return nil, err
		}
		r.RTT, r.Pending = durPtr(rtt), int64Ptr(pending)
		out = append(out, r)
	}
	return out, rows.Err()
}
