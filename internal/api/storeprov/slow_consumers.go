package storeprov

import (
	"context"
	"database/sql"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// slowCounterCTEs is shared by server summaries, both charts and investigation.
// Callers read a three-interval lookback, then select report endpoints in their
// half-open range. NULL reports break the baseline; missing rows may bridge at
// most three intervals. Process restarts partition independently.
func slowCounterCTEs(filter string) string {
	return `slow_reports AS (
 SELECT s.server_name, ss.server_id, e.epoch, e.started_at, ss.slow_consumers,
 lag(e.started_at) OVER w AS prev_at,
 lag(ss.slow_consumers) OVER w AS prev_counter
 FROM epochs e JOIN server_stats ss USING(epoch) JOIN servers s USING(server_id)
 WHERE e.started_at >= ? AND e.started_at < ? ` + filter + `
 WINDOW w AS (PARTITION BY ss.server_id ORDER BY e.epoch)
), slow_reasons AS (
 SELECT *, CASE
 WHEN prev_at IS NULL THEN 'Initial process report; no baseline'
 WHEN date_diff('millisecond', prev_at, started_at) > ` + maxGapToken + ` THEN 'Server report or collection gap exceeds three intervals'
 WHEN slow_consumers IS NULL OR prev_counter IS NULL THEN 'Counter not reported'
 WHEN slow_consumers < prev_counter THEN 'Counter reset; no increase inferred'
 ELSE '' END AS reason
 FROM slow_reports
), slow_evidence AS (
 SELECT *, CASE WHEN reason = '' THEN slow_consumers-prev_counter END AS delta
 FROM slow_reasons
)`
}

func (p *Provider) slowCounterLookback(start time.Time) time.Time {
	return start.Add(-3 * max(p.Interval, time.Second))
}

// GetSlowConsumerInvestigation uses a fixed number of set queries, never one
// per observation or event. All lists have independent bounds and truncation.
func (p *Provider) GetSlowConsumerInvestigation(ctx context.Context, req *v1.GetSlowConsumerInvestigationRequest) (*v1.GetSlowConsumerInvestigationResponse, error) {
	start, end := span(req.Range)
	out := &v1.GetSlowConsumerInvestigationResponse{Range: req.Range, Provenance: "Stored server_stats / epochs / servers; recorded client.disconnect events and slow-consumer finding transitions. Counters are not filtered by account; the account filter applies to recorded client evidence.", AttributionLimit: api.SlowConsumerAttributionLimit}
	limit := int(req.Limit)
	// ASVS 1.2.4: all bounds and names are parameters. Timestamp parameters
	// compare directly to columns; no host time-zone dependent arithmetic.
	rows, err := p.db().QueryContext(ctx, `
WITH `+p.metricQuery(metricSQL{query: slowCounterCTEs(`AND (? = '' OR s.server_name = ?)`)})+`
SELECT server_name, server_id, epoch, prev_at, started_at, delta, reason,
 sum(coalesce(delta,0)) OVER(), count(*) OVER()
FROM slow_evidence WHERE started_at >= ? AND (? = '' OR (server_id = ? AND epoch = ?)) AND (delta > 0 OR reason <> '')
ORDER BY started_at DESC, server_id, epoch DESC LIMIT ?`, p.slowCounterLookback(start), end, req.Server, req.Server, start, req.ObservationServerId, req.ObservationServerId, req.ObservationEpoch, limit+1)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		o := &v1.SlowConsumerObservation{}
		var prev sql.NullTime
		var at time.Time
		var delta sql.NullInt64
		var count int64
		if err = rows.Scan(&o.Server, &o.ServerId, &o.Epoch, &prev, &at, &delta, &o.UnavailableReason, &out.Detections, &count); err != nil {
			rows.Close()
			return nil, err
		}
		o.IntervalStart = ts(ntime(prev))
		o.IntervalEnd = ts(at.UTC())
		if delta.Valid {
			o.Increase = ptr(delta.Int64)
		}
		out.Observations = append(out.Observations, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out.Observations) > limit {
		out.ObservationsTruncated = true
		out.Observations = out.Observations[:limit]
	}
	// Matching here means same selected scope and time, never proof that an
	// event explains a particular aggregate increment. Payload IDs alone
	// establish a recorded connection identity; names are display labels.
	const matches = `e.kind = 'client.disconnect' AND contains(lower(coalesce(e.reason,'')), 'slow consumer')
 AND e.time >= ? AND e.time < ? AND (? = '' OR e.server_name = ?)
 AND (? = '' OR e.account = ? OR e.account = (SELECT any_value(account_name) FROM accounts WHERE account = ?))`
	args := []any{start, end, req.Server, req.Server, req.Account, req.Account, req.Account}
	rows, err = p.db().QueryContext(ctx, `SELECT `+eventCols+`, coalesce(json_extract_string(e.raw,'$.server.id'),''),
 coalesce(json_extract_string(e.raw,'$.client.id'),''), count(*) OVER() FROM events e WHERE `+matches+` ORDER BY e.time DESC,e.id DESC LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		v := &v1.SlowConsumerEvent{}
		v.Event, err = scanEvent(rows, &v.ServerId, &v.ConnectionId, &out.RecordedDisconnects)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out.Events = append(out.Events, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out.Events) > limit {
		out.EventsTruncated = true
		out.Events = out.Events[:limit]
	}
	rows, err = p.db().QueryContext(ctx, `SELECT t.finding_id,t.check_code,t.entity_type,t.entity_key,t.time,t.detail
 FROM finding_transitions t WHERE t.time >= ? AND t.time < ? AND (
 (t.check_code = 'server-009' AND t.entity_type = 'server' AND (? = '' OR t.entity_key = ?))
 OR (t.check_code = 'client-003' AND t.entity_type = 'client' AND EXISTS (
 SELECT 1 FROM events e WHERE `+matches+` AND e.client_name = t.entity_key)))
 ORDER BY t.time DESC,t.finding_id LIMIT ?`, append([]any{start, end, req.Server, req.Server}, append(args, limit+1)...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		f := &v1.SlowConsumerFinding{}
		var typ, key string
		var at time.Time
		if err = rows.Scan(&f.FindingId, &f.CheckCode, &typ, &key, &at, &f.Detail); err != nil {
			rows.Close()
			return nil, err
		}
		f.Entity = entityRef(typ, key)
		f.Time = ts(at.UTC())
		out.Findings = append(out.Findings, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out.Findings) > limit {
		out.FindingsTruncated = true
		out.Findings = out.Findings[:limit]
	}
	var first sql.NullTime
	var dropped bool
	if err = p.db().QueryRowContext(ctx, `SELECT (SELECT min(time) FROM events WHERE time < ?),
 EXISTS (SELECT 1 FROM events WHERE kind = 'collector.messages_dropped' AND time >= ? AND time < ?),
 (SELECT count(*) FROM epochs e WHERE e.started_at >= ? AND e.started_at < ? AND (e.partial OR len(e.missing_names)>0 OR EXISTS (
 SELECT 1 FROM server_stats ss JOIN servers s USING(server_id) WHERE ss.epoch=e.epoch AND ss.slow_consumers IS NULL AND (? = '' OR s.server_name = ?))))`, end, start, end, start, end, req.Server, req.Server).Scan(&first, &dropped, &out.IncompleteCollections); err != nil {
		return nil, err
	}
	out.EventsStart = ts(ntime(first))
	capture := &api.AccountsFacts{EpochAt: end, WindowStart: start}
	if err = p.captureGaps(ctx, capture); err != nil {
		return nil, err
	}
	out.EventsCaptured = capture.EventsCaptured
	out.CaptureGaps = capture.CaptureGaps || dropped || !first.Valid || first.Time.After(start)
	return out, nil
}
