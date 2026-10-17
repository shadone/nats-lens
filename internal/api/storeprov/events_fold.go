package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// richRequest reports whether a ListEvents request asks for anything a
// client that predates routine events, folding, facets and the histogram
// does not; such a request takes the old path unchanged.
func richRequest(req *v1.ListEventsRequest) bool {
	return req.GetHideRoutine() || req.GetFoldRepeats() || req.GetHistogramBuckets() > 0 || req.GetIncludeFacets()
}

// foldKey are the columns whose values a folded row shares. subject is
// what the stored columns do not name: a finding event's finding, a
// Kubernetes event's object.
const foldKey = `source, kind, server_name, account, stream, consumer, client_name, reason, subject`

// listEventsRich answers a request with routine events, folding, facets
// or a histogram.
func (p *Provider) listEventsRich(ctx context.Context, req *v1.ListEventsRequest, cur api.Cursor) (*v1.ListEventsResponse, error) {
	hide := req.GetHideRoutine()
	// A folded list is pinned to the events of its first page, so that an
	// event arriving between pages cannot move a row across the page
	// boundary.
	upto := int64(0)
	if req.GetFoldRepeats() {
		upto = cur.Upto
		if upto == 0 {
			if err := p.db().QueryRowContext(ctx, `SELECT coalesce(max(id), 0) FROM events`).Scan(&upto); err != nil {
				return nil, fmt.Errorf("newest event: %w", err)
			}
		}
	}
	base, args := p.richEvents(req, upto)
	res := &v1.ListEventsResponse{}
	var hidden int32
	if err := p.db().QueryRowContext(ctx, base+`SELECT count(*) FILTER (WHERE NOT routine OR NOT ?), count(*) FILTER (WHERE routine)
FROM b`, append(args, hide)...).Scan(&res.TotalCount, &hidden); err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}
	if hide {
		res.RoutineHidden = hidden
	}
	var err error
	if req.GetFoldRepeats() {
		err = p.foldedRows(ctx, req, cur, upto, res)
	} else {
		err = p.plainRows(ctx, req, cur, res)
	}
	if err != nil {
		return nil, err
	}
	if req.GetIncludeFacets() {
		if err := p.facets(ctx, req, upto, res); err != nil {
			return nil, err
		}
	}
	if hide {
		if res.RoutineGroups, err = p.routineGroups(ctx, req, upto); err != nil {
			return nil, err
		}
	}
	if n := req.GetHistogramBuckets(); n > 0 {
		if res.Histogram, err = p.histogram(ctx, req, upto, int(n)); err != nil {
			return nil, err
		}
	}
	return res, p.decorateEvents(ctx, res.Events)
}

// plainRows reads a page of events, one per row, with the routine flag.
func (p *Provider) plainRows(ctx context.Context, req *v1.ListEventsRequest, cur api.Cursor, res *v1.ListEventsResponse) error {
	base, args := p.richEvents(req, 0)
	q := base + `SELECT ` + eventCols + `, e.routine FROM b e WHERE (NOT e.routine OR NOT ?)`
	args = append(args, req.GetHideRoutine())
	if cur.ID != 0 {
		q += " AND (e.time < ? OR (e.time = ? AND e.id < ?))"
		args = append(args, cur.Time.UTC(), cur.Time.UTC(), cur.ID)
	}
	rows, err := p.db().QueryContext(ctx, q+` ORDER BY e.time DESC, e.id DESC LIMIT ?`, append(args, req.GetPageSize()+1)...)
	if err != nil {
		return fmt.Errorf("events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var routine bool
		ev, err := scanEvent(rows, &routine)
		if err != nil {
			return err
		}
		markRoutine(ev, routine)
		res.Events = append(res.Events, ev)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	res.NextPageToken = cutPage(res, req.GetPageSize(), 0)
	return nil
}

// foldedRows reads a page of folded rows. Each row is the newest event of
// a run of events with the same foldKey, each at most api.FoldGap after
// the previous one; pages continue below the newest event of the last
// row, among the events up to upto.
func (p *Provider) foldedRows(ctx context.Context, req *v1.ListEventsRequest, cur api.Cursor, upto int64, res *v1.ListEventsResponse) error {
	base, args := p.richEvents(req, upto)
	q := base + `, k AS (
  SELECT id, time, source, kind, server_name, account, stream, consumer, client_name, reason,
    CASE WHEN source = 'finding' THEN json_extract_string(raw, '$.finding_id')
      WHEN source = 'k8s' THEN json_extract_string(raw, '$.object.name') END AS subject,
    CASE WHEN source = 'k8s' THEN coalesce(TRY_CAST(json_extract_string(raw, '$.count') AS BIGINT), 1) ELSE 1 END AS n
  FROM b WHERE NOT routine OR NOT ?),
g AS (
  SELECT *, CASE WHEN time - lag(time) OVER (PARTITION BY ` + foldKey + ` ORDER BY time, id) <= INTERVAL ` +
		strconv.Itoa(int(api.FoldGap/time.Second)) + ` SECOND THEN 0 ELSE 1 END AS brk
  FROM k),
h AS (
  SELECT *, sum(brk) OVER (PARTITION BY ` + foldKey + ` ORDER BY time, id ROWS UNBOUNDED PRECEDING) AS grp FROM g),
r AS (
  SELECT id, time, sum(n) OVER w AS reps, min(time) OVER w AS first_time,
    row_number() OVER (PARTITION BY ` + foldKey + `, grp ORDER BY time DESC, id DESC) AS rn
  FROM h WINDOW w AS (PARTITION BY ` + foldKey + `, grp))
SELECT ` + eventCols + `, e.routine, r.reps, r.first_time FROM r JOIN b e ON e.id = r.id WHERE r.rn = 1`
	args = append(args, req.GetHideRoutine())
	if cur.ID != 0 {
		q += " AND (r.time < ? OR (r.time = ? AND r.id < ?))"
		args = append(args, cur.Time.UTC(), cur.Time.UTC(), cur.ID)
	}
	rows, err := p.db().QueryContext(ctx, q+` ORDER BY r.time DESC, r.id DESC LIMIT ?`, append(args, req.GetPageSize()+1)...)
	if err != nil {
		return fmt.Errorf("folded events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var routine bool
		var reps int64
		var first time.Time
		ev, err := scanEvent(rows, &routine, &reps, &first)
		if err != nil {
			return err
		}
		markRoutine(ev, routine)
		ev.Repeats, ev.FirstTime = int32(min(reps, 1<<31-1)), ts(first.UTC())
		res.Events = append(res.Events, ev)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	res.NextPageToken = cutPage(res, req.GetPageSize(), upto)
	return nil
}

// markRoutine sets the routine flag; a routine event is not coloured.
func markRoutine(ev *v1.Event, routine bool) {
	ev.Routine = routine
	if routine {
		ev.Severity = v1.Severity_SEVERITY_UNSPECIFIED
	}
}

// cutPage trims res to n rows when it holds one more, and returns the
// token of the next page, or "" on the last.
func cutPage(res *v1.ListEventsResponse, n int32, upto int64) string {
	if len(res.Events) <= int(n) {
		return ""
	}
	res.Events = res.Events[:n]
	l := res.Events[n-1]
	id, _ := strconv.ParseInt(l.GetId(), 10, 64)
	return api.EncodeCursor(api.Cursor{Time: l.GetTime().AsTime(), ID: id, Upto: upto})
}

// facets counts the events per source and per server, each without its
// own dimension's filter, and the events of the range.
func (p *Provider) facets(ctx context.Context, req *v1.ListEventsRequest, upto int64, res *v1.ListEventsResponse) error {
	bySource := proto.Clone(req).(*v1.ListEventsRequest)
	bySource.Sources = nil
	sources := map[string]int32{}
	if err := p.facetCounts(ctx, bySource, upto, "source", func(v string, n int32) {
		sources[sourceOf(v).String()] += n
	}); err != nil {
		return err
	}
	byServer := proto.Clone(req).(*v1.ListEventsRequest)
	if byServer.GetEntity().GetType() == v1.EntityType_ENTITY_TYPE_SERVER {
		byServer.Entity = nil
	}
	servers := map[string]int32{}
	if err := p.facetCounts(ctx, byServer, upto, "server_name", func(v string, n int32) { servers[v] += n }); err != nil {
		return err
	}
	res.Facets = append(sortedFacets("source", sources), sortedFacets("server", servers)...)
	start, end := span(req.GetRange())
	q, args := `SELECT count(*) FROM events WHERE time >= ? AND time < ?`, []any{start, end}
	if upto > 0 {
		q, args = q+` AND id <= ?`, append(args, upto)
	}
	return p.db().QueryRowContext(ctx, q, args...).Scan(&res.TotalUnfiltered)
}

// facetCounts reads the count of each value of col among the events req
// matches, routine events left out when it hides them.
func (p *Provider) facetCounts(ctx context.Context, req *v1.ListEventsRequest, upto int64, col string, add func(string, int32)) error {
	base, args := p.richEvents(req, upto)
	rows, err := p.db().QueryContext(ctx, base+`SELECT `+col+`, count(*) FROM b WHERE (NOT routine OR NOT ?) AND `+col+
		` IS NOT NULL GROUP BY 1`, append(args, req.GetHideRoutine())...)
	if err != nil {
		return fmt.Errorf("event facets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		var n int32
		if err := rows.Scan(&v, &n); err != nil {
			return err
		}
		add(v, n)
	}
	return rows.Err()
}

// sortedFacets lists a dimension's counts, largest first.
func sortedFacets(dim string, counts map[string]int32) []*v1.EventFacet {
	out := make([]*v1.EventFacet, 0, len(counts))
	for v, n := range counts {
		out = append(out, &v1.EventFacet{Dimension: dim, Value: v, Count: n})
	}
	slices.SortFunc(out, func(a, b *v1.EventFacet) int {
		return cmp.Or(cmp.Compare(b.GetCount(), a.GetCount()), cmp.Compare(a.GetValue(), b.GetValue()))
	})
	return out
}

// routineGroups counts the routine events req matches by kind and client,
// largest first.
func (p *Provider) routineGroups(ctx context.Context, req *v1.ListEventsRequest, upto int64) ([]*v1.RoutineGroup, error) {
	base, args := p.richEvents(req, upto)
	rows, err := p.db().QueryContext(ctx, base+`SELECT kind, coalesce(client_name, ''), count(*) FROM b WHERE routine
GROUP BY 1, 2 ORDER BY 3 DESC, 1, 2 LIMIT ?`, append(args, api.MaxRoutineGroups)...)
	if err != nil {
		return nil, fmt.Errorf("routine groups: %w", err)
	}
	defer rows.Close()
	var out []*v1.RoutineGroup
	for rows.Next() {
		g := &v1.RoutineGroup{}
		if err := rows.Scan(&g.Kind, &g.ClientName, &g.Count); err != nil {
			return nil, err
		}
		g.Label = api.RoutineLabel(g.GetKind(), g.GetClientName())
		out = append(out, g)
	}
	return out, rows.Err()
}

// histogram counts the events req matches, routine ones included, in n
// buckets over the range. The database groups them by what decides the
// severity; severityOf then decides it as for a listed event.
func (p *Provider) histogram(ctx context.Context, req *v1.ListEventsRequest, upto int64, n int) ([]*v1.EventBucket, error) {
	start, end := span(req.GetRange())
	width := api.HistogramWidth(end.Sub(start), n)
	out := make([]*v1.EventBucket, api.BucketCount(end.Sub(start), width))
	for i := range out {
		out[i] = &v1.EventBucket{Start: ts(start.Add(time.Duration(i) * width))}
	}
	base, args := p.richEvents(req, upto)
	rows, err := p.db().QueryContext(ctx, base+`SELECT (epoch_us(time) - ?) // ?, kind, routine,
  coalesce(reason ILIKE '%slow consumer%', false),
  coalesce(starts_with(consumer, 'JS_MIRROR_') OR starts_with(consumer, 'JS_SOURCE_'), false),
  CASE WHEN source = 'finding' THEN json_extract_string(raw, '$.severity') END, count(*)
FROM b GROUP BY ALL`, append(args, start.UnixMicro(), width.Microseconds())...)
	if err != nil {
		return nil, fmt.Errorf("event histogram: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var i int64
		var kind string
		var routine, slow, internal bool
		var findingSeverity sql.NullString
		var count int32
		if err := rows.Scan(&i, &kind, &routine, &slow, &internal, &findingSeverity, &count); err != nil {
			return nil, err
		}
		if i < 0 || i >= int64(len(out)) {
			continue
		}
		reason, consumer := "", ""
		if slow {
			reason = "slow consumer"
		}
		if internal {
			consumer = "JS_MIRROR_"
		}
		sev := severityOf(kind, reason, consumer)
		if findingSeverity.Valid {
			sev = severityOfText[findingSeverity.String]
		}
		histogramClass(out[i], routine, sev, count)
	}
	return out, rows.Err()
}
