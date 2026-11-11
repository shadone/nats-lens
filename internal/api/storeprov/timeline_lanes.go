package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// subjectLanes fills the server, leader and client lanes of res, and
// whether system events and JetStream advisories were captured, inside
// the timeline's read transaction.
func (p *Provider) subjectLanes(ctx context.Context, req *v1.GetStatusTimelineRequest, res *v1.GetStatusTimelineResponse) error {
	r, entity, readID := req.GetRange(), req.GetEntity(), res.GetEventReadId()
	_, end := span(r)
	evs, cut, err := p.laneEvents(ctx, r, entity, api.ServerLaneKinds, readID)
	if err != nil {
		return err
	}
	restarts, restartCut, err := p.restarts(ctx, r, entity)
	if err != nil {
		return err
	}
	items := make([]*v1.LaneEvent, 0, len(evs)+len(restarts))
	for _, ev := range evs {
		items = append(items, api.LaneEventOf(ev))
	}
	res.Servers, res.ServersTruncated = api.NewestLane(laneLimit, cut || restartCut, items, restarts)

	if evs, cut, err = p.laneEvents(ctx, r, entity, api.LeaderLaneKinds, readID); err != nil {
		return err
	}
	leaders := make([]*v1.LaneEvent, 0, len(evs))
	for _, ev := range evs {
		leaders = append(leaders, api.LaneEventOf(ev))
	}
	res.Leaders, res.LeadersTruncated = api.NewestLane(laneLimit, cut, leaders)

	if res.Clients, err = p.clientLane(ctx, r, req.GetBucket().AsDuration(), entity, readID); err != nil {
		return err
	}
	// LIMIT 1 lets the scan stop at the first stored event of the source.
	captured := func(source string) (bool, error) {
		var n int
		err := p.db().QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM events WHERE source = ? AND time < ? AND id <= ? LIMIT 1)`,
			source, end, readID).Scan(&n)
		return n > 0, err
	}
	if res.SystemEventsCaptured, err = captured("sys"); err != nil {
		return fmt.Errorf("timeline system events captured: %w", err)
	}
	if res.JetstreamEventsCaptured, err = captured("jetstream"); err != nil {
		return fmt.Errorf("timeline advisories captured: %w", err)
	}
	return nil
}

// restarts reads the newest laneLimit restarts that started in the range,
// as server-004 finds them: a process of a server name with an earlier
// one, at its reported start time or, without one, when it was first
// collected.
func (p *Provider) restarts(ctx context.Context, r *v1.TimeRange, entity *v1.EntityRef) ([]*v1.LaneEvent, bool, error) {
	if !api.RestartMatches(entity, entity.GetKey()) {
		return nil, false, nil
	}
	server := ""
	if entity.GetType() == v1.EntityType_ENTITY_TYPE_SERVER {
		server = entity.GetKey()
	}
	start, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
WITH procs AS (
    SELECT server_id, server_name, coalesce(version, '') AS version, coalesce(start_time, first_seen_at) AS started,
        lag(server_id) OVER w AS prev_id, coalesce(lag(version) OVER w, '') AS prev_version
    FROM servers
    WINDOW w AS (PARTITION BY server_name ORDER BY first_epoch)
)
SELECT server_name, prev_id, server_id, started, prev_version, version
FROM procs
WHERE prev_id IS NOT NULL AND started >= ? AND started < ? AND (? = '' OR server_name = ?)
ORDER BY started DESC, server_id DESC LIMIT ?`, start, end, server, server, laneLimit+1)
	if err != nil {
		return nil, false, fmt.Errorf("timeline restarts: %w", err)
	}
	defer rows.Close()
	var out []*v1.LaneEvent
	for rows.Next() {
		var x api.ServerRestart
		if err := rows.Scan(&x.Server, &x.OldID, &x.NewID, &x.Started, &x.OldVersion, &x.NewVersion); err != nil {
			return nil, false, err
		}
		if len(out) == laneLimit {
			return out, true, nil
		}
		out = append(out, api.RestartLaneEvent(x))
	}
	return out, false, rows.Err()
}

// clientLane counts the client events of the range per bucket, as
// api.ClientLane classifies them: one row per bucket, kind and disconnect
// reason, so the read is bounded by the buckets, not by the events.
func (p *Provider) clientLane(ctx context.Context, r *v1.TimeRange, bucket time.Duration, entity *v1.EntityRef, readID int64) ([]*v1.ClientLaneBucket, error) {
	start, _ := span(r)
	where, args := eventFilter(&v1.ListEventsRequest{Range: r, Kinds: api.ClientLaneKinds, Entity: entity, EventReadId: &readID})
	us := bucket.Microseconds()
	if us <= 0 {
		return nil, nil
	}
	rows, err := p.db().QueryContext(ctx, `
SELECT CAST(floor((epoch_us(e.time) - ?) / ?) AS BIGINT) AS b, e.kind,
    CASE WHEN e.kind = 'client.disconnect' THEN coalesce(e.reason, '') ELSE '' END AS reason,
    sum(CASE WHEN e.kind = 'client.churn' THEN coalesce(TRY_CAST(e.raw->>'connects' AS BIGINT), 0) ELSE 1 END)::BIGINT
FROM events e WHERE `+where+`
GROUP BY ALL`, append([]any{start.UnixMicro(), us}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("timeline client lane: %w", err)
	}
	defer rows.Close()
	var counts []api.ClientEventCount
	for rows.Next() {
		var c api.ClientEventCount
		var b sql.NullInt64
		if err := rows.Scan(&b, &c.Kind, &c.Reason, &c.N); err != nil {
			return nil, err
		}
		c.Bucket = int(b.Int64)
		counts = append(counts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return api.ClientLane(counts), nil
}
