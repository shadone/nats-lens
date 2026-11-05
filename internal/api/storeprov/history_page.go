package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// DataHistory reads the facts of Data history in three queries, none per
// epoch: the history and its counts, the range's times without
// collections, and the collector events around the range.
func (p *Provider) DataHistory(ctx context.Context, req *v1.GetDataHistoryRequest) (*api.HistoryFacts, error) {
	start, end := span(req.GetRange())
	f := &api.HistoryFacts{Start: start, End: end, Now: p.Now(), Interval: p.Interval, Retention: p.Retention}
	if p.ArchiveStatus != nil {
		f.Archive = p.ArchiveStatus()
		f.Archive.Enabled = true
	}
	if p.Checks != nil {
		for _, c := range p.Checks.Checks() {
			if lb := p.Checks.Lookback(c.Code); lb > 0 {
				f.Lookbacks = append(f.Lookbacks, api.CheckLookback{Code: c.Code, Lookback: lb})
			}
		}
	}
	if err := p.historyCounts(ctx, f); err != nil {
		return nil, err
	}
	var err error
	if f.Collections, err = p.collectionGaps(ctx, start, end); err != nil {
		return nil, err
	}
	if f.Events, err = p.historyEvents(ctx, start, end); err != nil {
		return nil, err
	}
	return f, nil
}

// historyCounts reads the oldest and newest collections, the oldest
// event, the range's collections and partial ones, and the firing
// findings already true at the history start (the finding page's rule,
// with HistoryStartSlack).
func (p *Provider) historyCounts(ctx context.Context, f *api.HistoryFacts) error {
	var first, last, events sql.NullTime
	err := p.db().QueryRowContext(ctx, `
WITH h AS (SELECT min(started_at) AS first FROM epochs)
SELECT (SELECT first FROM h), (SELECT max(started_at) FROM epochs), (SELECT min(time) FROM events),
    (SELECT count(*) FROM epochs WHERE started_at >= ? AND started_at < ?),
    (SELECT count(*) FROM epochs WHERE started_at >= ? AND started_at < ? AND partial),
    (SELECT count(*) FROM findings, h WHERE state = 'firing'
        AND (condition_since < h.first OR (since >= h.first AND epoch_us(since) < epoch_us(h.first) + ?)))`,
		f.Start, f.End, f.Start, f.End, api.HistoryStartSlack.Microseconds()).Scan(&first, &last, &events, &f.Epochs, &f.PartialEpochs, &f.FindingsAtStart)
	if err != nil {
		return fmt.Errorf("data history counts: %w", err)
	}
	f.HistoryStart, f.NewestEpochAt, f.EventsStart = ntime(first), ntime(last), ntime(events)
	return nil
}

// historyEvents reads the collector events of HistoryEventKinds from
// HistoryEventsLookback before the range to its end; the collector.gap and
// collector.k8s_gap events that began earlier and ended in or after the
// range; and the losses still open at the start of that read: for each
// connection the loss that began it, as Replay's events read finds them
// (for the collector's own connection only not_connected and
// connection_closed), and the collector.k8s_not_watching that no
// collector.k8s_gap or collector.started followed. With their raw payload,
// in time order.
func (p *Provider) historyEvents(ctx context.Context, start, end time.Time) ([]*v1.Event, error) {
	from := start.Add(-api.HistoryEventsLookback)
	kinds := api.HistoryEventKinds
	args := []any{from, end, from, start}
	for _, k := range kinds {
		args = append(args, k)
	}
	rows, err := p.db().QueryContext(ctx, `
WITH s AS (
  SELECT e.*, coalesce(json_extract_string(e.raw, '$.connection'), '') AS conn FROM events e
  WHERE e.source = 'collector' AND e.time < ? AND e.kind IN ('collector.started', 'collector.disconnected', 'collector.not_connected',
      'collector.connection_closed', 'collector.reconnected', 'collector.k8s_not_watching', 'collector.k8s_gap')
),
open_conn AS (
  SELECT * FROM s
  WHERE s.kind IN ('collector.disconnected', 'collector.not_connected', 'collector.connection_closed') AND s.conn <> ''
    AND (s.conn <> 'collector' OR s.kind <> 'collector.disconnected')
    AND s.time > coalesce((SELECT max(time) FROM s r WHERE r.kind = 'collector.started' OR (r.kind = 'collector.reconnected' AND r.conn = s.conn)),
        TIMESTAMP '1970-01-01')
  QUALIFY row_number() OVER (PARTITION BY s.conn ORDER BY s.time, s.id) = 1
),
open_k8s AS (
  SELECT * FROM s
  WHERE s.kind = 'collector.k8s_not_watching'
    AND s.time > coalesce((SELECT max(time) FROM s r WHERE r.kind IN ('collector.started', 'collector.k8s_gap')), TIMESTAMP '1970-01-01')
  QUALIFY row_number() OVER (ORDER BY s.time, s.id) = 1
),
e AS (
  SELECT * EXCLUDE (conn) FROM open_conn
  UNION ALL SELECT * EXCLUDE (conn) FROM open_k8s
  UNION ALL SELECT * FROM events e WHERE e.source = 'collector' AND e.time <= ?
    AND (e.time >= ? OR (e.kind IN ('collector.gap', 'collector.k8s_gap') AND e.received_at >= ?))
    AND e.kind IN (?`+strings.Repeat(", ?", len(kinds)-1)+`)
)
SELECT `+eventCols+`, coalesce(e.raw::VARCHAR, '') FROM e
ORDER BY e.time, e.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("data history events: %w", err)
	}
	defer rows.Close()
	var out []*v1.Event
	for rows.Next() {
		var raw string
		ev, err := scanEvent(rows, &raw)
		if err != nil {
			return nil, err
		}
		ev.Raw = raw
		out = append(out, ev)
	}
	return out, rows.Err()
}
