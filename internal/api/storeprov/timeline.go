package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// timelineHits are the SQL common table expressions shared by the
// timeline's queries. Parameters, in order: start and end of the range
// (runs), start (before), start and end (ids), end and start (tr), now
// twice (muted_now), and the bucket start and width for the bucket index.
//
//   - runs: every check run of an epoch in the range;
//   - ids: the findings with a transition in the range, or whose last
//     transition before it left them firing;
//   - seg: their firing segments, from a firing or changed transition to
//     the next transition;
//   - hits: each ok run with a finding firing at its time, the severity
//     (1 info, 2 warning, 3 critical), whether a mute covered the finding
//     at the run's time and whether one covers it now, and whether the run
//     is its check's newest in the range.
const timelineHits = `
WITH runs AS (
    SELECT r.check_code, e.started_at AS t, r.status = 'ok' AS ok
    FROM check_runs r JOIN epochs e USING (epoch)
    WHERE e.started_at >= ? AND e.started_at < ?
),
newest AS (SELECT check_code, max(t) AS t FROM runs GROUP BY check_code),
before AS (
    SELECT finding_id, arg_max(kind, epoch) AS kind, max(epoch) AS epoch
    FROM finding_transitions WHERE time < ? GROUP BY finding_id
),
ids AS (
    SELECT DISTINCT finding_id FROM finding_transitions WHERE time >= ? AND time < ?
    UNION
    SELECT finding_id FROM before WHERE kind <> 'resolved'
),
tr AS (
    SELECT t.finding_id, t.check_code, t.entity_type, t.entity_key, t.time, t.kind, t.severity,
        lead(t.time) OVER (PARTITION BY t.finding_id ORDER BY t.epoch) AS until
    FROM finding_transitions t JOIN ids USING (finding_id)
    WHERE t.time < ? AND (t.time >= ? OR (t.finding_id, t.epoch) IN (
        SELECT b.finding_id, b.epoch FROM before b JOIN ids USING (finding_id)))
),
seg AS (SELECT * FROM tr WHERE kind <> 'resolved'),
hits AS (
    SELECT r.check_code, r.t, s.finding_id, s.entity_type, s.entity_key,
        CASE s.severity WHEN 'critical' THEN 3 WHEN 'warning' THEN 2 ELSE 1 END AS sev,
        EXISTS (SELECT 1 FROM mutes m WHERE m.check_code = s.check_code
            AND (m.entity_type IS NULL OR (m.entity_type = s.entity_type AND m.entity_key = s.entity_key))
            AND m.created_at <= r.t AND m.expires_at > r.t AND (m.unmuted_at IS NULL OR m.unmuted_at > r.t)) AS muted,
        EXISTS (SELECT 1 FROM mutes m WHERE m.check_code = s.check_code
            AND (m.entity_type IS NULL OR (m.entity_type = s.entity_type AND m.entity_key = s.entity_key))
            AND m.created_at <= ? AND m.expires_at > ? AND m.unmuted_at IS NULL) AS muted_now,
        r.t = n.t AS last
    FROM runs r JOIN seg s ON s.check_code = r.check_code AND r.t >= s.time AND (s.until IS NULL OR r.t < s.until)
    JOIN newest n ON n.check_code = r.check_code
    WHERE r.ok
)`

// bucketOf is the bucket index of the time column col: two parameters,
// the range's start and the bucket width in microseconds.
func bucketOf(col string) string {
	return `CAST(floor(date_diff('microsecond', ?::TIMESTAMP, ` + col + `) / ?) AS BIGINT)`
}

// cellState is one row's aggregate for one bucket.
type cellState struct {
	ok          bool // an ok run in the bucket
	sev         int  // worst unmuted severity firing, 0 for none
	muted       bool // a muted finding fired
	nowSev      int  // at the newest run: worst severity firing without a current mute
	nowMuted    bool // at the newest run: a finding firing that a mute covers now
	newestOK    bool // the newest run was ok (runs only)
	hasNewestOK bool
}

func (c cellState) status() v1.Status {
	switch {
	case !c.ok:
		return v1.Status_STATUS_NO_DATA
	case c.sev > 0:
		return sevStatus(c.sev)
	case c.muted:
		return v1.Status_STATUS_MUTED
	}
	return v1.Status_STATUS_OK
}

func sevStatus(sev int) v1.Status {
	switch sev {
	case 3:
		return v1.Status_STATUS_CRITICAL
	case 2:
		return v1.Status_STATUS_WARNING
	}
	return v1.Status_STATUS_INFO
}

// rowKey is a timeline row: a check, or a check and one finding's entity.
type rowKey struct {
	code, entityType, entityKey string
}

// GetStatusTimeline draws, per check, the worst state per bucket: the
// worst firing finding of the epochs in the bucket whose run was ok;
// muted when every finding firing then was under a mute in force at that
// run; ok when none fired; no data when the check did not run ok in any
// epoch of the bucket (disabled, failed, or no epoch). now is the newest
// run's state with the mutes in force now. The findings come from the
// transitions, so a finding that resolved and fired again keeps every
// episode. With an entity, there is one row per finding of the entity, and
// with include_ok one for every check of the entity's type. The lanes hold
// the NATS events and finding transitions, and the Kubernetes events; the
// windows are the node drains and upgrades.
func (p *Provider) GetStatusTimeline(ctx context.Context, req *v1.GetStatusTimelineRequest) (*v1.GetStatusTimelineResponse, error) {
	if _, ok := p.readDB.(*sql.Tx); ok {
		return p.statusTimeline(ctx, req)
	}
	beginner, ok := p.db().(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return nil, fmt.Errorf("%w: Timeline read transaction unavailable", api.ErrFailedPrecondition)
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	at := p.Now()
	// Construct the read view explicitly: Provider owns mutex-bearing caches.
	view := &Provider{St: p.St, Interval: p.Interval, Checks: p.Checks, ClientPods: p.ClientPods, Kubernetes: p.Kubernetes, Clock: func() time.Time { return at }, readDB: tx}
	res, err := view.statusTimeline(ctx, req)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

func (p *Provider) statusTimeline(ctx context.Context, req *v1.GetStatusTimelineRequest) (*v1.GetStatusTimelineResponse, error) {
	start, end := span(req.GetRange())
	bucket := req.GetBucket().AsDuration()
	n := api.BucketCount(end.Sub(start), bucket)
	res := &v1.GetStatusTimelineResponse{Range: proto.Clone(req.Range).(*v1.TimeRange), Start: req.GetRange().GetStart(), Bucket: req.GetBucket(), BucketCount: int32(n)}
	if err := p.db().QueryRowContext(ctx, `SELECT coalesce(max(epoch),0) FROM finding_transitions`).Scan(&res.FindingReadEpoch); err != nil {
		return nil, err
	}
	var newestEvent int64
	if err := p.db().QueryRowContext(ctx, `SELECT coalesce(max(id),0) FROM events`).Scan(&newestEvent); err != nil {
		return nil, err
	}
	res.EventReadId = proto.Int64(newestEvent)
	if req.EventReadId != nil {
		if *req.EventReadId < 0 {
			return nil, api.ErrInvalid
		}
		if *req.EventReadId > newestEvent {
			return nil, fmt.Errorf("%w: event capture boundary is unavailable", api.ErrFailedPrecondition)
		}
		res.EventReadId = proto.Int64(*req.EventReadId)
	}
	var err error
	if res.Nats, res.NatsTruncated, res.FindingTruncated, err = p.lanes(ctx, req.GetRange(), req.GetEntity(), res.GetEventReadId()); err != nil {
		return nil, err
	}
	if res.Kubernetes, res.KubernetesTruncated, err = p.k8sLane(ctx, req.GetRange(), req.GetEntity(), res.GetEventReadId()); err != nil {
		return nil, err
	}
	if res.Windows, err = p.k8sWindows(ctx, req.GetRange(), res.GetEventReadId()); err != nil {
		return nil, err
	}
	if res.Rows, err = p.timelineRows(ctx, req.GetRange(), bucket, req.GetEntity(), req.GetIncludeOk()); err != nil {
		return nil, err
	}
	return res, nil
}

// timelineRows are the rows of the status timeline, worst first: one per
// check, or with an entity one per finding of the entity and, with
// includeOK, one for every check of its type. Without includeOK a row that
// never failed is left out.
func (p *Provider) timelineRows(ctx context.Context, r *v1.TimeRange, bucket time.Duration, entity *v1.EntityRef, includeOK bool) ([]*v1.TimelineRow, error) {
	start, end := span(r)
	n := api.BucketCount(end.Sub(start), bucket)
	now := p.Now()
	us := bucket.Microseconds()
	hitArgs := []any{start, end, start, start, end, end, start, now, now}

	// Runs per check and bucket, and whether each check's newest run was ok.
	runs := map[string]map[int64]cellState{}
	rows, err := p.db().QueryContext(ctx, timelineHits+`
SELECT r.check_code, `+bucketOf("r.t")+` AS b, bool_or(r.ok), bool_or(r.ok AND r.t = n.t), bool_or(r.t = n.t)
FROM runs r JOIN newest n USING (check_code) GROUP BY ALL`, append(hitArgs, start, us)...)
	if err != nil {
		return nil, fmt.Errorf("timeline runs: %w", err)
	}
	for rows.Next() {
		var code string
		var b int64
		var c cellState
		if err := rows.Scan(&code, &b, &c.ok, &c.newestOK, &c.hasNewestOK); err != nil {
			rows.Close()
			return nil, err
		}
		if runs[code] == nil {
			runs[code] = map[int64]cellState{}
		}
		runs[code][b] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Firing findings per row and bucket.
	perEntity := entity.GetType() != v1.EntityType_ENTITY_TYPE_UNSPECIFIED && entity.GetType() != v1.EntityType_ENTITY_TYPE_SYSTEM
	group := `check_code, '', ''`
	if perEntity {
		group = `check_code, entity_type, entity_key`
	}
	hits := map[rowKey]map[int64]cellState{}
	rows, err = p.db().QueryContext(ctx, timelineHits+`
SELECT `+group+`, `+bucketOf("t")+` AS b,
    coalesce(max(sev) FILTER (WHERE NOT muted), 0), bool_or(muted),
    coalesce(max(sev) FILTER (WHERE last AND NOT muted_now), 0), bool_or(last AND muted_now)
FROM hits GROUP BY ALL`, append(hitArgs, start, us)...)
	if err != nil {
		return nil, fmt.Errorf("timeline findings: %w", err)
	}
	for rows.Next() {
		var k rowKey
		var b int64
		var c cellState
		if err := rows.Scan(&k.code, &k.entityType, &k.entityKey, &b, &c.sev, &c.muted, &c.nowSev, &c.nowMuted); err != nil {
			rows.Close()
			return nil, err
		}
		if perEntity && !matchesEntity(entityRef(k.entityType, k.entityKey), entity) {
			continue
		}
		if hits[k] == nil {
			hits[k] = map[int64]cellState{}
		}
		hits[k][b] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	keys := make([]rowKey, 0, len(runs))
	if perEntity {
		withRow := map[string]bool{}
		for k := range hits {
			keys = append(keys, k)
			withRow[k.code] = true
		}
		if includeOK {
			for code := range runs {
				if c, ok := p.lookup(code); ok && entityTypeOf(c.Entity) == entity.GetType() && !withRow[code] {
					keys = append(keys, rowKey{code: code, entityType: c.Entity, entityKey: entity.GetKey()})
				}
			}
		}
	} else {
		for code := range runs {
			keys = append(keys, rowKey{code: code})
		}
	}
	var out []*v1.TimelineRow
	for _, k := range keys {
		row := p.timelineRow(k, perEntity, runs[k.code], hits[k], n)
		if includeOK || failed(row.GetCells()) {
			out = append(out, row)
		}
	}
	slices.SortFunc(out, func(a, b *v1.TimelineRow) int {
		return cmp.Or(cmp.Compare(statusRank(a.GetNow()), statusRank(b.GetNow())),
			cmp.Compare(severityRank(a.GetSeverity()), severityRank(b.GetSeverity())), cmp.Compare(a.GetKey(), b.GetKey()))
	})
	return out, nil
}

// timelineRow builds one row from its check's runs and its findings per
// bucket.
func (p *Provider) timelineRow(k rowKey, perEntity bool, runs, hits map[int64]cellState, n int) *v1.TimelineRow {
	row := &v1.TimelineRow{Key: k.code, CheckCode: k.code, CheckName: k.code, Cells: make([]v1.Status, n), Now: v1.Status_STATUS_NO_DATA}
	if c, ok := p.lookup(k.code); ok {
		row.CheckName, row.Severity = c.Name, severityOfText[c.Severity]
	}
	if perEntity {
		row.Entity = entityRef(k.entityType, k.entityKey)
		row.Key = k.code + ":" + k.entityKey
	}
	for i := range row.Cells {
		c := runs[int64(i)]
		h := hits[int64(i)]
		c.sev, c.muted = h.sev, h.muted
		row.Cells[i] = c.status()
		if c.hasNewestOK {
			now := cellState{ok: c.newestOK, sev: h.nowSev, muted: h.nowMuted}
			row.Now = now.status()
		}
	}
	return row
}

// failed reports whether any cell is worse than ok or no data.
func failed(cells []v1.Status) bool {
	for _, c := range cells {
		if c != v1.Status_STATUS_OK && c != v1.Status_STATUS_NO_DATA {
			return true
		}
	}
	return false
}

func (p *Provider) lookup(code string) (struct{ Name, Severity, Entity string }, bool) {
	var out struct{ Name, Severity, Entity string }
	if p.Checks == nil {
		return out, false
	}
	c, ok := p.Checks.Lookup(code)
	if !ok {
		return out, false
	}
	out.Name, out.Severity, out.Entity = c.Name, c.Severity, c.Entity
	return out, true
}
