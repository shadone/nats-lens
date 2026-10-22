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

// epochBounds are the lowest and highest epoch that started in [from,
// to); ok is false when none did. A scan bounded by them reads only
// those epochs' rows.
func (p *Provider) epochBounds(ctx context.Context, from, to time.Time) (lo, hi int64, ok bool, err error) {
	var l, h sql.NullInt64
	if err := p.db().QueryRowContext(ctx, `SELECT min(epoch), max(epoch) FROM epochs WHERE started_at >= `+tsParam(1)+` AND started_at < `+tsParam(2),
		us(from), us(to)).
		Scan(&l, &h); err != nil {
		return 0, 0, false, fmt.Errorf("epoch bounds: %w", err)
	}
	return l.Int64, h.Int64, l.Valid, nil
}

// us is a time as microseconds since 1970 in UTC, the form every time
// takes as a parameter of the micro queries (see tsParam).
func us(t time.Time) int64 { return t.UnixMicro() }

// tsParam is the TIMESTAMP of parameter n given by us. DuckDB may type a
// time.Time parameter as TIMESTAMP WITH TIME ZONE, depending on the query,
// and compare it with the stored UTC timestamps in the session's time
// zone: a page on a laptop in Los Angeles then read hours 7 hours off.
// Microseconds carry no zone.
func tsParam(n int) string { return fmt.Sprintf("make_timestamp($%d::BIGINT)", n) }

// microStates maps micro_requests.state to the API's.
var microStates = map[string]v1.MicroRequestState{
	"ok": v1.MicroRequestState_MICRO_REQUEST_STATE_OK, "no_responders": v1.MicroRequestState_MICRO_REQUEST_STATE_NO_RESPONDERS,
	"timeout": v1.MicroRequestState_MICRO_REQUEST_STATE_TIMEOUT, "error": v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR,
}

// microAccounts reads the accounts asked in the epoch: those with a
// $SRV.STATS request in it, with that request and their newest $SRV.INFO
// at or before it. An account under two labels is read once: an ok
// request first, then the most telling failure (an error with its reason,
// a timeout, no responders).
func (p *Provider) microAccounts(ctx context.Context, e epochInfo) ([]api.MicroAccountFact, error) {
	lo, _, _, err := p.epochBounds(ctx, e.startedAt.Add(-api.SubscriptionsMaxAge), e.startedAt.Add(time.Nanosecond))
	if err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH st AS (
  SELECT account, state, replies, error FROM micro_requests WHERE epoch = $1 AND request = 'STATS'
  QUALIFY row_number() OVER (PARTITION BY account ORDER BY CASE state WHEN 'ok' THEN 0 WHEN 'error' THEN 1 WHEN 'timeout' THEN 2 ELSE 3 END, label) = 1
), inf AS (
  SELECT r.account, r.epoch, r.state, r.replies, r.error FROM micro_requests r
  WHERE r.epoch BETWEEN $2 AND $1 AND r.request = 'INFO' AND r.account IN (SELECT account FROM st)
  QUALIFY row_number() OVER (PARTITION BY r.account ORDER BY r.epoch DESC, CASE r.state WHEN 'ok' THEN 0 WHEN 'error' THEN 1 WHEN 'timeout' THEN 2 ELSE 3 END, r.label) = 1
)
SELECT st.account, st.state, st.replies, coalesce(st.error, ''), inf.state, coalesce(inf.replies, 0), coalesce(inf.error, ''), ie.started_at
FROM st LEFT JOIN inf USING (account) LEFT JOIN epochs ie ON ie.epoch = inf.epoch ORDER BY st.account`, e.epoch, lo)
	if err != nil {
		return nil, fmt.Errorf("micro accounts: %w", err)
	}
	defer rows.Close()
	var out []api.MicroAccountFact
	for rows.Next() {
		var a api.MicroAccountFact
		var state string
		var infoState sql.NullString
		var infoAt sql.NullTime
		if err := rows.Scan(&a.Account, &state, &a.Stats.Replies, &a.Stats.Error, &infoState, &a.Info.Replies, &a.Info.Error, &infoAt); err != nil {
			return nil, err
		}
		a.Stats.State, a.Stats.At = microStates[state], e.startedAt
		if infoState.Valid {
			a.Info.State, a.Info.At = microStates[infoState.String], infoAt.Time.UTC()
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// microPoints selects the endpoint reports of the instances that
// answered the epoch ($1), of the epochs $2 to $3 that started in [$4,
// $5): their counters, with their instance's start time from micro_stats
// and the epoch's start as t. The endpoints are those of the relation the
// query names eps; the rate window reads only the epoch's instances, as a
// gone instance's run would count a restart twice.
const microPoints = `
  SELECT x.account, x.service, x.endpoint, x.instance_id, s.started, ep.started_at AS t,
    x.num_requests AS r, x.num_errors AS er, x.processing_time_ns AS ns
  FROM micro_endpoints x
  JOIN micro_stats s ON s.epoch = x.epoch AND s.account = x.account AND s.service = x.service AND s.instance_id = x.instance_id
  JOIN epochs ep ON ep.epoch = x.epoch
  JOIN (SELECT account, service, instance_id FROM micro_stats WHERE epoch = $1) cur
    ON cur.account = x.account AND cur.service = x.service AND cur.instance_id = x.instance_id
  JOIN eps ON eps.account = x.account AND eps.service = x.service AND eps.endpoint = x.endpoint
  WHERE x.epoch BETWEEN $2 AND $3 AND ep.started_at >= make_timestamp($4::BIGINT) AND ep.started_at < make_timestamp($5::BIGINT)`

// microAnswered reads the services that answered the epoch's $SRV.STATS:
// each instance with its start, and the version and description of its
// newest INFO (else the version its STATS reported); each endpoint with
// its subject and queue group, its first last error by instance ID, its
// increase over the api.RateSpan before the range's end
// (api.MicroWindowIncrease's rule), its level from the hourly rollup
// (api.MicroLevel) and, when slow, the start of its slow run
// (api.MicroSlowStart). Go reads one row per instance and endpoint, and
// per hour and collection for the slow endpoints only.
func (p *Provider) microAnswered(ctx context.Context, e epochInfo, r *v1.TimeRange) ([]api.MicroServiceFact, error) {
	out, index, err := p.microInstances(ctx, e)
	if err != nil || len(out) == 0 {
		return out, err
	}
	_, end := span(r)
	from := end.Add(-api.RateSpan)
	eps, err := p.microEndpoints(ctx, e)
	if err != nil {
		return nil, err
	}
	if err := p.microWindow(ctx, e, from, end, eps); err != nil {
		return nil, err
	}
	if err := p.microLevels(ctx, from, eps); err != nil {
		return nil, err
	}
	if err := p.microSlowStarts(ctx, e, eps); err != nil {
		return nil, err
	}
	for _, k := range eps.order {
		x := eps.byKey[k]
		i := index[x.account+"\x00"+x.service]
		out[i].Endpoints = append(out[i].Endpoints, x.fact)
	}
	return out, nil
}

// microInstances reads the instances that answered the epoch's STATS, by
// service, and the index of each service by account and name.
func (p *Provider) microInstances(ctx context.Context, e epochInfo) ([]api.MicroServiceFact, map[string]int, error) {
	lo, _, _, err := p.epochBounds(ctx, e.startedAt.Add(-api.SubscriptionsMaxAge), e.startedAt.Add(time.Nanosecond))
	if err != nil {
		return nil, nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH s AS (
  SELECT account, service, instance_id, version, started FROM micro_stats WHERE epoch = $1
), i AS (
  SELECT m.account, m.service, m.instance_id, arg_max(m.version, m.epoch) AS version, arg_max(m.description, m.epoch) AS description
  FROM micro_instances m JOIN s USING (account, service, instance_id) WHERE m.epoch BETWEEN $2 AND $1 GROUP BY ALL
)
SELECT s.account, s.service, s.instance_id, coalesce(i.version, s.version, ''), coalesce(i.description, ''), s.started
FROM s LEFT JOIN i USING (account, service, instance_id) ORDER BY 1, 2, 3`, e.epoch, lo)
	if err != nil {
		return nil, nil, fmt.Errorf("micro instances: %w", err)
	}
	defer rows.Close()
	var out []api.MicroServiceFact
	index := map[string]int{}
	for rows.Next() {
		var acc, svc string
		var in api.MicroInstanceFact
		var started sql.NullTime
		if err := rows.Scan(&acc, &svc, &in.ID, &in.Version, &in.Description, &started); err != nil {
			return nil, nil, err
		}
		if started.Valid {
			t := started.Time.UTC()
			in.Started = &t
		}
		k := acc + "\x00" + svc
		if _, ok := index[k]; !ok {
			index[k] = len(out)
			out = append(out, api.MicroServiceFact{Account: acc, Name: svc})
		}
		out[index[k]].Instances = append(out[index[k]].Instances, in)
	}
	return out, index, rows.Err()
}

// microEndpoint is one endpoint being read.
type microEndpoint struct {
	account, service string
	fact             api.MicroEndpointFact
}

// microEndpointSet are the epoch's endpoints by key, in order.
type microEndpointSet struct {
	byKey map[string]*microEndpoint
	order []string
}

func microKey(account, service, endpoint string) string {
	return account + "\x00" + service + "\x00" + endpoint
}

// microEndpoints reads the endpoints the epoch's STATS reported: subject
// and queue group (the first any instance gave) and the first last error
// by instance ID.
func (p *Provider) microEndpoints(ctx context.Context, e epochInfo) (microEndpointSet, error) {
	set := microEndpointSet{byKey: map[string]*microEndpoint{}}
	rows, err := p.db().QueryContext(ctx, `
SELECT account, service, endpoint, coalesce(min(subject), ''), coalesce(min(queue_group), ''),
  coalesce(arg_min(last_error, instance_id) FILTER (WHERE coalesce(last_error, '') <> ''), ''),
  coalesce(min(instance_id) FILTER (WHERE coalesce(last_error, '') <> ''), '')
FROM micro_endpoints WHERE epoch = $1 GROUP BY ALL ORDER BY 1, 2, 3`, e.epoch)
	if err != nil {
		return set, fmt.Errorf("micro endpoints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		x := &microEndpoint{}
		f := &x.fact
		if err := rows.Scan(&x.account, &x.service, &f.Name, &f.Subject, &f.QueueGroup, &f.LastError, &f.LastErrorInstance); err != nil {
			return set, err
		}
		k := microKey(x.account, x.service, f.Name)
		set.byKey[k] = x
		set.order = append(set.order, k)
	}
	return set, rows.Err()
}

// epsOf is the eps relation of microPoints: the epoch's endpoints.
const epsOf = `eps AS (SELECT DISTINCT account, service, endpoint FROM micro_endpoints WHERE epoch = $1)`

// microWindow reads each endpoint's increase over [from, end): per
// instance of the epoch, its newest run (one start time, no counter
// falling) with two reports at different times, from its first to its last
// report, summed.
func (p *Provider) microWindow(ctx context.Context, e epochInfo, from, end time.Time, eps microEndpointSet) error {
	lo, hi, ok, err := p.epochBounds(ctx, from, end)
	if err != nil || !ok {
		return err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH `+epsOf+`, pts AS (`+microPoints+`
), marked AS (
  SELECT *, CASE WHEN started IS DISTINCT FROM lag(started) OVER w OR r < lag(r) OVER w OR er < lag(er) OVER w OR ns < lag(ns) OVER w
    THEN 1 ELSE 0 END AS brk
  FROM pts WINDOW w AS (PARTITION BY account, service, endpoint, instance_id ORDER BY t)
), runs AS (
  SELECT *, sum(brk) OVER (PARTITION BY account, service, endpoint, instance_id ORDER BY t ROWS UNBOUNDED PRECEDING) AS run FROM marked
), newest AS (
  SELECT * FROM runs QUALIFY run = max(run) OVER (PARTITION BY account, service, endpoint, instance_id)
), per AS (
  SELECT account, service, endpoint, instance_id, arg_max(r, t) - arg_min(r, t) AS dr, arg_max(er, t) - arg_min(er, t) AS de,
    arg_max(ns, t) - arg_min(ns, t) AS dn, date_diff('millisecond', min(t), max(t)) AS ms
  FROM newest GROUP BY ALL HAVING count(*) >= 2 AND max(t) > min(t)
)
SELECT account, service, endpoint, sum(dr * 1000.0 / ms), sum(dr)::DOUBLE, sum(de)::DOUBLE, sum(dn)::DOUBLE FROM per GROUP BY ALL`,
		e.epoch, lo, hi, us(from), us(end))
	if err != nil {
		return fmt.Errorf("micro window: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var acc, svc, ep string
		var w api.MicroIncrease
		var rate float64
		if err := rows.Scan(&acc, &svc, &ep, &rate, &w.Requests, &w.Errors, &w.ProcessingNs); err != nil {
			return err
		}
		if x := eps.byKey[microKey(acc, svc, ep)]; x != nil {
			w.PerSecond = &rate
			x.fact.Window = w
		}
	}
	return rows.Err()
}

// microLevels reads each endpoint's level from the hourly rollup
// (micro_endpoint_hours, summed over the instances as the epochs are
// written): the median of the averages of the hours that start at or
// after api.MicroLevelSpan before from and end at or before from, with at
// least api.MicroLevelMinHours of them. Instances do not matter: the
// level survives a redeploy.
func (p *Provider) microLevels(ctx context.Context, from time.Time, eps microEndpointSet) error {
	rows, err := p.db().QueryContext(ctx, `
SELECT account, service, endpoint, median(processing_time_ns / requests / 1e6)
FROM micro_endpoint_hours WHERE hour >= `+tsParam(1)+` AND hour + INTERVAL 1 HOUR <= `+tsParam(2)+` AND requests > 0
GROUP BY ALL HAVING count(*) >= `+fmt.Sprint(api.MicroLevelMinHours), us(from.Add(-api.MicroLevelSpan)), us(from))
	if err != nil {
		return fmt.Errorf("micro levels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var acc, svc, ep string
		var level float64
		if err := rows.Scan(&acc, &svc, &ep, &level); err != nil {
			return err
		}
		if x := eps.byKey[microKey(acc, svc, ep)]; x != nil {
			x.fact.LevelMs = &level
		}
	}
	return rows.Err()
}

// slowEndpoints are the endpoints whose window is slow against their
// level, by api.MicroIsSlow.
func slowEndpoints(eps microEndpointSet) []*microEndpoint {
	var out []*microEndpoint
	for _, k := range eps.order {
		x := eps.byKey[k]
		w := x.fact.Window
		if w.Requests <= 0 {
			continue
		}
		avg := w.ProcessingNs / w.Requests / 1e6
		if api.MicroIsSlow(&avg, x.fact.LevelMs) {
			out = append(out, x)
		}
	}
	return out
}

// valuesOf is a VALUES list of (account, service, endpoint, extra) rows
// for the slow endpoints, its placeholders numbered from after args.
func valuesOf(slow []*microEndpoint, args []any, extra func(*microEndpoint) any, cast string) (string, []any) {
	var values []string
	for _, x := range slow {
		n := len(args)
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d::%s)", n+1, n+2, n+3, n+4, cast))
		args = append(args, x.account, x.service, x.fact.Name, extra(x))
	}
	return strings.Join(values, ", "), args
}

// microSlowStarts sets, for each slow endpoint, the start of its slow
// run by api.MicroSlowStart: the hourly rollup's averages from
// api.MicroLevelSpan before the epoch up to the epoch's hour, and, for an
// endpoint with a fast hour, the collections' own averages over that hour
// and the next (api.MicroSlowStartSpan), up to the epoch, of every
// instance: each the sum of the increases between consecutive reports of
// one instance and start time that end in it, an increase where a counter
// fell left out (api.MicroDeltas). The walk is bounded by the span, and
// raw reports are read for at most two hours per slow endpoint.
func (p *Provider) microSlowStarts(ctx context.Context, e epochInfo, eps microEndpointSet) error {
	slow := slowEndpoints(eps)
	if len(slow) == 0 {
		return nil
	}
	values, args := valuesOf(slow, []any{us(e.startedAt.Add(-api.MicroLevelSpan)), us(e.startedAt)}, func(*microEndpoint) any { return "" }, "VARCHAR")
	rows, err := p.db().QueryContext(ctx, `
WITH v(account, service, endpoint, x) AS (VALUES `+values+`)
SELECT h.account, h.service, h.endpoint, h.hour, h.processing_time_ns / h.requests / 1e6
FROM micro_endpoint_hours h JOIN v USING (account, service, endpoint)
WHERE h.hour >= `+tsParam(1)+` AND h.hour <= `+tsParam(2)+` AND h.requests > 0 ORDER BY 1, 2, 3, 4`, args...)
	if err != nil {
		return fmt.Errorf("micro slow hours: %w", err)
	}
	hours := map[string][]api.MicroAverage{}
	for rows.Next() {
		var acc, svc, ep string
		var a api.MicroAverage
		if err := rows.Scan(&acc, &svc, &ep, &a.At, &a.Ms); err != nil {
			rows.Close()
			return err
		}
		a.At = a.At.UTC()
		k := microKey(acc, svc, ep)
		hours[k] = append(hours[k], a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	fast := map[string]time.Time{}
	var withFast []*microEndpoint
	lo := e.startedAt
	for _, x := range slow {
		k := microKey(x.account, x.service, x.fact.Name)
		if f := api.MicroLastFastHour(hours[k], *x.fact.LevelMs); f != nil {
			fast[k] = *f
			withFast = append(withFast, x)
			lo = earliestTime(lo, *f)
		} else {
			x.fact.SlowSince = api.MicroSlowStart(hours[k], nil, *x.fact.LevelMs)
		}
	}
	if len(withFast) == 0 {
		return nil
	}
	// The report before the hour gives the hour's first increase.
	elo, ehi, ok, err := p.epochBounds(ctx, lo.Add(-api.MicroMissingWindow), e.startedAt.Add(time.Nanosecond))
	if err != nil || !ok {
		return err
	}
	values, args = valuesOf(withFast, []any{elo, ehi, us(e.startedAt)}, func(x *microEndpoint) any {
		return fast[microKey(x.account, x.service, x.fact.Name)].UnixMicro()
	}, "BIGINT")
	rows, err = p.db().QueryContext(ctx, `
WITH v0(account, service, endpoint, us) AS (VALUES `+values+`
), v AS (SELECT account, service, endpoint, make_timestamp(us) AS f FROM v0
), pts AS (
  SELECT x.account, x.service, x.endpoint, x.instance_id, s.started, ep.started_at AS t, v.f,
    x.num_requests AS r, x.num_errors AS er, x.processing_time_ns AS ns
  FROM micro_endpoints x
  JOIN v ON v.account = x.account AND v.service = x.service AND v.endpoint = x.endpoint
  JOIN micro_stats s ON s.epoch = x.epoch AND s.account = x.account AND s.service = x.service AND s.instance_id = x.instance_id
  JOIN epochs ep ON ep.epoch = x.epoch
  WHERE x.epoch BETWEEN $1 AND $2 AND ep.started_at >= v.f - INTERVAL 1 HOUR AND ep.started_at <= `+tsParam(3)+`
    AND ep.started_at < v.f + INTERVAL `+fmt.Sprint(int(api.MicroSlowStartSpan.Hours()))+` HOUR
), d AS (
  SELECT account, service, endpoint, t, f, r - lag(r) OVER w AS dr, er - lag(er) OVER w AS de, ns - lag(ns) OVER w AS dn
  FROM pts WINDOW w AS (PARTITION BY account, service, endpoint, instance_id, started ORDER BY t)
)
SELECT account, service, endpoint, t, sum(dn) / sum(dr) / 1e6 FROM d
WHERE t >= f AND dr >= 0 AND de >= 0 AND dn >= 0 GROUP BY ALL HAVING sum(dr) > 0 ORDER BY 1, 2, 3, 4`, args...)
	if err != nil {
		return fmt.Errorf("micro slow collections: %w", err)
	}
	defer rows.Close()
	cols := map[string][]api.MicroAverage{}
	for rows.Next() {
		var acc, svc, ep string
		var a api.MicroAverage
		if err := rows.Scan(&acc, &svc, &ep, &a.At, &a.Ms); err != nil {
			return err
		}
		a.At = a.At.UTC()
		k := microKey(acc, svc, ep)
		cols[k] = append(cols[k], a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, x := range withFast {
		k := microKey(x.account, x.service, x.fact.Name)
		x.fact.SlowSince = api.MicroSlowStart(hours[k], cols[k], *x.fact.LevelMs)
	}
	return nil
}

func earliestTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// goneAsked reads the instances that answered $SRV.STATS within
// api.SubscriptionsMaxAge before the epoch and not in it: each with its
// last answer, the account's next STATS request after it, the account's
// ok requests since that went without it, the service's instances in
// that last answer, and the name of the connection that held its $SRV
// subscriptions in the newest subscription collection of the two hours
// before the epoch that lists it. api.FinishMicro keeps those of the hour before the epoch, and
// older ones while their subscriptions are still listed (a hang).
func (p *Provider) goneAsked(ctx context.Context, e epochInfo) ([]api.MicroGoneFact, error) {
	lo, _, ok, err := p.epochBounds(ctx, e.startedAt.Add(-api.SubscriptionsMaxAge), e.startedAt.Add(time.Nanosecond))
	if err != nil || !ok {
		return nil, err
	}
	// Connections are named from the collections of the two hours before.
	slo, _, _, err := p.epochBounds(ctx, e.startedAt.Add(-2*api.MicroMissingWindow), e.startedAt.Add(time.Nanosecond))
	if err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH asked AS (
  SELECT DISTINCT account FROM micro_requests WHERE epoch = $1 AND request = 'STATS'
), reqs AS (
  SELECT r.account, r.epoch, min(ep.started_at) AS started_at, bool_or(r.state = 'ok') AS ok FROM micro_requests r JOIN epochs ep USING (epoch)
  WHERE r.epoch BETWEEN $2 AND $1 AND r.request = 'STATS' AND r.account IN (SELECT account FROM asked) GROUP BY ALL
), st AS (
  SELECT epoch, account, service, instance_id FROM micro_stats WHERE epoch BETWEEN $2 AND $1 AND account IN (SELECT account FROM asked)
), last AS (
  SELECT account, service, instance_id, max(epoch) AS epoch FROM st GROUP BY ALL HAVING max(epoch) < $1
), cols AS (
  SELECT x.epoch FROM epochs x WHERE x.epoch BETWEEN $3 AND $1
    AND EXISTS (SELECT 1 FROM epoch_endpoints q WHERE q.epoch = x.epoch AND q.endpoint = 'SUBSZ' AND q.state = 'ok')
), i AS (`+srvInstances+`
), held AS (
  SELECT i.service, i.id, arg_max(c.name, i.epoch) AS name
  FROM i JOIN (SELECT DISTINCT service, instance_id FROM last) l ON l.service = i.service AND l.instance_id = i.id
  LEFT JOIN connections c ON c.epoch = i.epoch AND c.server_id = i.server_id AND c.cid = i.cid GROUP BY ALL
)
SELECT l.account, l.service, l.instance_id, le.started_at,
  (SELECT min(q.started_at) FROM reqs q WHERE q.account = l.account AND q.epoch > l.epoch),
  (SELECT count(*) FROM reqs q WHERE q.account = l.account AND q.epoch > l.epoch AND q.ok),
  coalesce(h.name, ''), (SELECT count(*) FROM st o WHERE o.epoch = l.epoch AND o.account = l.account AND o.service = l.service)
FROM last l JOIN epochs le ON le.epoch = l.epoch
LEFT JOIN held h ON h.service = l.service AND h.id = l.instance_id
ORDER BY 1, 2, 3`, e.epoch, lo, slo)
	if err != nil {
		return nil, fmt.Errorf("gone micro instances asked: %w", err)
	}
	defer rows.Close()
	var out []api.MicroGoneFact
	for rows.Next() {
		var g api.MicroGoneFact
		var since sql.NullTime
		if err := rows.Scan(&g.Account, &g.Service, &g.ID, &g.LastSeen, &since, &g.Absent, &g.Connection, &g.InstancesThen); err != nil {
			return nil, err
		}
		g.LastSeen, g.Since = g.LastSeen.UTC(), since.Time.UTC()
		out = append(out, g)
	}
	return out, rows.Err()
}
