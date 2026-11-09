package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// FindingEpisodeQuery selects retained occurrences overlapping [From, To).
// FindingID and StartEpoch select a known occurrence without substituting a
// later recurrence. Limits are validated independently of public API callers.
type FindingEpisodeQuery struct {
	From, To           time.Time
	FindingID          string
	StartEpoch         int64
	FirstRetainedEpoch int64
	// ReadEpoch freezes transition membership across pages, not the entire database.
	ReadEpoch                        int64
	CheckCode, EntityType, EntityKey string
	Limit, Offset                    int
}

// FindingEpisode preserves the observed start identity independently of the
// current findings row. Unknown starts are retained fragments, not invented onsets.
type FindingEpisode struct {
	Finding            Finding
	StartKnown         bool
	FirstRetainedEpoch int64
	LastRetainedEpoch  int64
	TransitionCount    int
	// Resolution and latest retained state are distinct from the state at To.
	ResolutionAt                time.Time
	ResolutionEpoch             int64
	LatestState, LatestSeverity string
	LatestStartEpoch            int64
	LatestObservedAt            time.Time
}

type FindingEpisodePage struct {
	Signature string
	Items     []FindingEpisode
	Total     int
	ReadEpoch int64
}

// FindingEpisodes is a separate projection: FindingsAt intentionally retains
// its established newest-episode-per-finding contract.
func (s *Store) FindingEpisodes(ctx context.Context, q FindingEpisodeQuery) (FindingEpisodePage, error) {
	if q.From.IsZero() || !q.To.After(q.From) || q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 1<<20 || q.StartEpoch < 0 || q.FirstRetainedEpoch < 0 || q.ReadEpoch < 0 || ((q.StartEpoch > 0 || q.FirstRetainedEpoch > 0) && q.FindingID == "") || len(q.FindingID) > 4096 || len(q.CheckCode) > 128 || len(q.EntityType) > 64 || len(q.EntityKey) > 4096 {
		return FindingEpisodePage{}, ErrInvalidFindingEpisodeQuery
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FindingEpisodePage{}, err
	}
	defer tx.Rollback()
	var newestEpoch int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(epoch),0) FROM finding_transitions`).Scan(&newestEpoch); err != nil {
		return FindingEpisodePage{}, err
	}
	readEpoch := q.ReadEpoch
	if readEpoch > newestEpoch {
		return FindingEpisodePage{}, ErrInvalidFindingEpisodeQuery
	}
	if readEpoch == 0 {
		readEpoch = newestEpoch
	}
	rows, err := tx.QueryContext(ctx, findingEpisodesSQL, readEpoch, q.FindingID, q.FindingID, q.CheckCode, q.CheckCode,
		q.EntityType, q.EntityType, q.EntityKey, q.EntityType, q.EntityKey, episodeBoundaryMicros(q.To), episodeBoundaryMicros(q.From), episodeBoundaryMicros(q.To), q.StartEpoch, q.StartEpoch, q.FirstRetainedEpoch, q.FirstRetainedEpoch, q.Limit, q.Offset)
	if err != nil {
		return FindingEpisodePage{}, fmt.Errorf("finding episodes: %w", err)
	}
	defer rows.Close()
	out := FindingEpisodePage{ReadEpoch: readEpoch}
	bytes := 0
	var sqlBytes int64
	for rows.Next() {
		var id, code, typ, key, kind, severity, detail sql.NullString
		var firstEpoch, startEpoch, lastEpoch, lastRetained sql.NullInt64
		var transitionCount sql.NullInt64
		var firstAt, lastAt sql.NullTime
		var value sql.NullFloat64
		var cause sql.NullInt64
		var resolutionAt, latestAt sql.NullTime
		var resolutionEpoch, latestStart sql.NullInt64
		var latestState, latestSeverity sql.NullString
		if err := rows.Scan(&out.Total, &out.Signature, &sqlBytes, &id, &code, &typ, &key, &firstEpoch, &startEpoch, &firstAt, &lastEpoch, &lastAt, &kind, &severity, &detail, &value, &cause, &resolutionAt, &resolutionEpoch, &latestState, &latestSeverity, &latestStart, &latestAt, &lastRetained, &transitionCount); err != nil {
			return FindingEpisodePage{}, err
		}
		if sqlBytes > 8<<20 {
			return FindingEpisodePage{}, ErrFindingEpisodeReadLimit
		}
		if !id.Valid {
			continue
		}
		bytes += len(id.String) + len(code.String) + len(typ.String) + len(key.String) + len(detail.String)
		if bytes > 8<<20 {
			return FindingEpisodePage{}, ErrFindingEpisodeReadLimit
		}
		e := FindingEpisode{LastRetainedEpoch: lastRetained.Int64, TransitionCount: int(transitionCount.Int64), StartKnown: startEpoch.Valid, FirstRetainedEpoch: firstEpoch.Int64, ResolutionEpoch: resolutionEpoch.Int64, LatestState: latestState.String, LatestSeverity: latestSeverity.String, LatestStartEpoch: latestStart.Int64}
		if resolutionAt.Valid {
			e.ResolutionAt = resolutionAt.Time.UTC()
		}
		if latestAt.Valid {
			e.LatestObservedAt = latestAt.Time.UTC()
		}
		f := &e.Finding
		f.ID, f.CheckCode, f.EntityType, f.EntityKey = id.String, code.String, typ.String, key.String
		f.SinceEpoch, f.Since = startEpoch.Int64, firstAt.Time.UTC()
		f.LastSeenEpoch, f.TransitionedAt = lastEpoch.Int64, lastAt.Time.UTC()
		f.LastTransition, f.Severity, f.Detail = kind.String, severity.String, detail.String
		f.State = StateFiring
		if kind.String == TransitionResolved {
			f.State, f.ResolvedAt = StateResolved, lastAt.Time.UTC()
		}
		if value.Valid {
			v := value.Float64
			f.Value = &v
		}
		f.CauseEventID = cause.Int64
		out.Items = append(out.Items, e)
	}
	if err := rows.Err(); err != nil {
		return FindingEpisodePage{}, err
	}
	if err := tx.Commit(); err != nil {
		return FindingEpisodePage{}, err
	}
	return out, nil
}

var ErrInvalidFindingEpisodeQuery = errors.New("invalid finding episode query")
var ErrFindingEpisodeReadLimit = errors.New("finding episode read exceeds byte budget")

// Windowing precedes time membership: an occurrence is defined by recorded
// firing transitions in epoch order, not by mutable current finding rows.
// All caller-controlled scope, bounds and paging values remain parameters.
const findingEpisodesSQL = `WITH ordered AS (
 SELECT *,sum(CASE WHEN kind='firing' THEN 1 ELSE 0 END) OVER(
  PARTITION BY finding_id ORDER BY epoch ROWS UNBOUNDED PRECEDING) AS occurrence
 FROM finding_transitions WHERE epoch<=? AND (?='' OR finding_id=?) AND (?='' OR check_code=?)
 AND (?='' OR (entity_type=? AND entity_key=?) OR (?='stream' AND entity_type='consumer' AND starts_with(entity_key,?||'/')))
), episodes AS (
 SELECT finding_id,max(check_code) AS check_code,max(entity_type) AS entity_type,max(entity_key) AS entity_key,
 min(epoch) AS first_epoch,max(epoch) AS last_epoch,count(*) AS transition_count,max(epoch) FILTER(WHERE kind='firing') AS start_epoch,
 arg_min(time,epoch) AS first_at,max(time) FILTER(WHERE kind='resolved') AS resolved_at,
 max(epoch) FILTER(WHERE kind='resolved') AS resolved_epoch,
 arg_max(struct_pack(epoch,time,kind,severity,detail,value,cause_event_id),epoch) FILTER(WHERE time<make_timestamp(?::BIGINT)) AS last
 FROM ordered GROUP BY finding_id,occurrence
), matching AS (
 SELECT * FROM episodes WHERE (resolved_at IS NULL OR resolved_at>make_timestamp(?::BIGINT)) AND first_at<make_timestamp(?::BIGINT) AND last IS NOT NULL
 AND (?=0 OR start_epoch=?) AND (?=0 OR first_epoch=?)
), page AS (
 SELECT * FROM matching ORDER BY first_epoch DESC,finding_id LIMIT ? OFFSET ?
)
SELECT totals.n,totals.membership,budget.n,p.finding_id,p.check_code,p.entity_type,p.entity_key,p.first_epoch,p.start_epoch,p.first_at,
 p.last.epoch,p.last.time,p.last.kind,p.last.severity,p.last.detail,p.last.value,p.last.cause_event_id,
 p.resolved_at,p.resolved_epoch,f.state,f.severity,f.since_epoch,latest.started_at,p.last_epoch,p.transition_count
FROM (SELECT count(*) AS n,
 coalesce(bit_xor(hash(finding_id,first_epoch,start_epoch,last_epoch,transition_count)),0)::VARCHAR || ':' ||
 coalesce(sum(hash(finding_id,first_epoch,start_epoch,last_epoch,transition_count))::VARCHAR,'0') AS membership
 FROM matching) totals
CROSS JOIN (SELECT coalesce(sum(octet_length(encode(finding_id))+octet_length(encode(check_code))+
 octet_length(encode(entity_type))+octet_length(encode(entity_key))+octet_length(encode(last.detail))),0) AS n FROM page) budget
LEFT JOIN page p ON budget.n<=8388608
LEFT JOIN findings f ON f.id=p.finding_id
LEFT JOIN epochs latest ON latest.epoch=f.last_seen_epoch
ORDER BY p.first_epoch DESC,p.finding_id`

// Stored timestamps have microsecond precision. Ceiling either bound preserves
// the exact >=start / <end predicate when an API boundary has finer precision.
func episodeBoundaryMicros(t time.Time) int64 {
	us := t.UnixMicro()
	if t.Nanosecond()%1000 != 0 {
		us++
	}
	return us
}
