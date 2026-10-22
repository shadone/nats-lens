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
	if err := p.db().QueryRowContext(ctx, `SELECT min(epoch), max(epoch) FROM epochs WHERE started_at >= ? AND started_at < ?`, from, to).
		Scan(&l, &h); err != nil {
		return 0, 0, false, fmt.Errorf("epoch bounds: %w", err)
	}
	return l.Int64, h.Int64, l.Valid, nil
}

// microStates maps micro_requests.state to the API's.
var microStates = map[string]v1.MicroRequestState{
	"ok": v1.MicroRequestState_MICRO_REQUEST_STATE_OK, "no_responders": v1.MicroRequestState_MICRO_REQUEST_STATE_NO_RESPONDERS,
	"timeout": v1.MicroRequestState_MICRO_REQUEST_STATE_TIMEOUT, "error": v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR,
}

// microAccounts reads the accounts asked in the epoch: those with a
// $SRV.STATS request in it, with that request and their newest $SRV.INFO
// at or before it. An account under two labels is read once, an answered
// request first.
func (p *Provider) microAccounts(ctx context.Context, e epochInfo, account string) ([]api.MicroAccountFact, error) {
	lo, _, _, err := p.epochBounds(ctx, e.startedAt.Add(-api.SubscriptionsMaxAge), e.startedAt.Add(time.Nanosecond))
	if err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH st AS (
  SELECT account, state, replies, error FROM micro_requests WHERE epoch = $1 AND request = 'STATS' AND ($2 = '' OR account = $2)
  QUALIFY row_number() OVER (PARTITION BY account ORDER BY state = 'ok' DESC, label) = 1
), inf AS (
  SELECT r.account, r.epoch, r.state, r.replies, r.error FROM micro_requests r
  WHERE r.epoch BETWEEN $3 AND $1 AND r.request = 'INFO' AND r.account IN (SELECT account FROM st)
  QUALIFY row_number() OVER (PARTITION BY r.account ORDER BY r.epoch DESC, r.state = 'ok' DESC, r.label) = 1
)
SELECT st.account, st.state, st.replies, coalesce(st.error, ''), inf.state, coalesce(inf.replies, 0), coalesce(inf.error, ''), ie.started_at
FROM st LEFT JOIN inf USING (account) LEFT JOIN epochs ie ON ie.epoch = inf.epoch ORDER BY st.account`, e.epoch, account, lo)
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

// microPoints selects the endpoint reports of the instances that answered
// the epoch ($1), in the account ($2, empty for every one), of the epochs
// $3 to $4 that started in [$5, $6): their counters, with their instance's
// start time from micro_stats and the epoch's start as t. The endpoints
// are those of the relation the query names eps.
const microPoints = `
  SELECT x.account, x.service, x.endpoint, x.instance_id, s.started, ep.started_at AS t,
    x.num_requests AS r, x.num_errors AS er, x.processing_time_ns AS ns
  FROM micro_endpoints x
  JOIN micro_stats s ON s.epoch = x.epoch AND s.account = x.account AND s.service = x.service AND s.instance_id = x.instance_id
  JOIN epochs ep ON ep.epoch = x.epoch
  JOIN (SELECT account, service, instance_id FROM micro_stats WHERE epoch = $1 AND ($2 = '' OR account = $2)) cur
    ON cur.account = x.account AND cur.service = x.service AND cur.instance_id = x.instance_id
  JOIN eps ON eps.account = x.account AND eps.service = x.service AND eps.endpoint = x.endpoint
  WHERE x.epoch BETWEEN $3 AND $4 AND ep.started_at >= $5 AND ep.started_at < $6`

// microAnswered reads the services that answered the epoch's $SRV.STATS:
// each instance with its start, and the version and description of its
// newest INFO (else the version its STATS reported); each endpoint with
// its subject and queue group, its first last error by instance ID, its
// increase over the api.RateSpan before the range's end
// (api.MicroWindowIncrease's rule), its level (api.MicroLevel over
// api.MicroHourlyAverages) and, when slow, the start of its slow run
// (api.MicroSlowSince over api.MicroCollectionAverages). Every rule runs
// in SQL; Go reads one row per instance and per endpoint.
func (p *Provider) microAnswered(ctx context.Context, e epochInfo, r *v1.TimeRange, account string) ([]api.MicroServiceFact, error) {
	out, index, err := p.microInstances(ctx, e, account)
	if err != nil || len(out) == 0 {
		return out, err
	}
	_, end := span(r)
	from := end.Add(-api.RateSpan)
	eps, err := p.microEndpoints(ctx, e, account)
	if err != nil {
		return nil, err
	}
	if err := p.microWindow(ctx, e, account, from, end, eps); err != nil {
		return nil, err
	}
	if err := p.microLevels(ctx, e, account, from, eps); err != nil {
		return nil, err
	}
	if err := p.microSlowSince(ctx, e, account, from, eps); err != nil {
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
func (p *Provider) microInstances(ctx context.Context, e epochInfo, account string) ([]api.MicroServiceFact, map[string]int, error) {
	lo, _, _, err := p.epochBounds(ctx, e.startedAt.Add(-api.SubscriptionsMaxAge), e.startedAt.Add(time.Nanosecond))
	if err != nil {
		return nil, nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH s AS (
  SELECT account, service, instance_id, version, started FROM micro_stats WHERE epoch = $1 AND ($2 = '' OR account = $2)
), i AS (
  SELECT m.account, m.service, m.instance_id, arg_max(m.version, m.epoch) AS version, arg_max(m.description, m.epoch) AS description
  FROM micro_instances m JOIN s USING (account, service, instance_id) WHERE m.epoch BETWEEN $3 AND $1 GROUP BY ALL
)
SELECT s.account, s.service, s.instance_id, coalesce(i.version, s.version, ''), coalesce(i.description, ''), s.started
FROM s LEFT JOIN i USING (account, service, instance_id) ORDER BY 1, 2, 3`, e.epoch, account, lo)
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
func (p *Provider) microEndpoints(ctx context.Context, e epochInfo, account string) (microEndpointSet, error) {
	set := microEndpointSet{byKey: map[string]*microEndpoint{}}
	rows, err := p.db().QueryContext(ctx, `
SELECT account, service, endpoint, coalesce(min(subject), ''), coalesce(min(queue_group), ''),
  coalesce(arg_min(last_error, instance_id) FILTER (WHERE coalesce(last_error, '') <> ''), ''),
  coalesce(min(instance_id) FILTER (WHERE coalesce(last_error, '') <> ''), '')
FROM micro_endpoints WHERE epoch = $1 AND ($2 = '' OR account = $2) GROUP BY ALL ORDER BY 1, 2, 3`, e.epoch, account)
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

// epsOf is the eps relation of microPoints: the epoch's endpoints in the
// account.
const epsOf = `eps AS (SELECT DISTINCT account, service, endpoint FROM micro_endpoints WHERE epoch = $1 AND ($2 = '' OR account = $2))`

// microWindow reads each endpoint's increase over [from, end): per
// instance of the epoch, its newest run (one start time, no counter
// falling) with two reports at different times, from its first to its last
// report, summed.
func (p *Provider) microWindow(ctx context.Context, e epochInfo, account string, from, end time.Time, eps microEndpointSet) error {
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
		e.epoch, account, lo, hi, from, end)
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

// microLevels reads each endpoint's level: the median of its hourly
// averages over the api.MicroLevelSpan before from, with at least
// api.MicroLevelMinHours of them. An hour's increase per instance and
// start time is its last report's counters less the last report before
// the hour (its first report when there is none), which is the sum of the
// increases between consecutive reports that end in the hour, as
// api.MicroDeltas takes them; the framework never lets a counter fall
// within one start time, and an hour where one did is left out. The rows
// are reduced per hour before anything is compared, so 7 days of 30-second
// reports are grouped, not walked.
func (p *Provider) microLevels(ctx context.Context, e epochInfo, account string, from time.Time, eps microEndpointSet) error {
	start := from.Add(-api.MicroLevelSpan)
	lo, hi, ok, err := p.epochBounds(ctx, start, from)
	if err != nil || !ok {
		return err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH `+epsOf+`, pts AS (`+microPoints+`
), g AS (
  SELECT account, service, endpoint, instance_id, started, date_trunc('hour', t) AS h,
    arg_min(r, t) AS fr, arg_max(r, t) AS lr, arg_min(ns, t) AS fn, arg_max(ns, t) AS ln
  FROM pts GROUP BY ALL
), d AS (
  SELECT account, service, endpoint, h, lr - coalesce(lag(lr) OVER w, fr) AS dr, ln - coalesce(lag(ln) OVER w, fn) AS dn
  FROM g WINDOW w AS (PARTITION BY account, service, endpoint, instance_id, started ORDER BY h)
), hourly AS (
  SELECT account, service, endpoint, h, sum(dn) / sum(dr) / 1e6 AS ms FROM d WHERE dr >= 0 AND dn >= 0
  GROUP BY ALL HAVING sum(dr) > 0
)
SELECT account, service, endpoint, median(ms) FROM hourly GROUP BY ALL HAVING count(*) >= `+fmt.Sprint(api.MicroLevelMinHours),
		e.epoch, account, lo, hi, start, from)
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

// microSlowSince reads, for each slow endpoint (its window's average
// against its level, by api.MicroIsSlow), the start of its slow run: the
// collections' averages over the api.MicroLevelSpan before from up to the
// epoch, each the sum of the increases between consecutive reports of one
// instance and start time that end in it (an increase where a counter fell
// left out, as api.MicroDeltas splits the run there); the run of slow
// collections that ends at the newest slow one, its first. Only slow
// endpoints are read.
func (p *Provider) microSlowSince(ctx context.Context, e epochInfo, account string, from time.Time, eps microEndpointSet) error {
	var values []string
	args := []any{e.epoch, account, int64(0), int64(0), from.Add(-api.MicroLevelSpan), e.startedAt.Add(time.Nanosecond)}
	for _, k := range eps.order {
		x := eps.byKey[k]
		w := x.fact.Window
		if w.Requests <= 0 {
			continue
		}
		avg := w.ProcessingNs / w.Requests / 1e6
		if !api.MicroIsSlow(&avg, x.fact.LevelMs) {
			continue
		}
		n := len(args)
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d::DOUBLE)", n+1, n+2, n+3, n+4))
		args = append(args, x.account, x.service, x.fact.Name, *x.fact.LevelMs)
	}
	if len(values) == 0 {
		return nil
	}
	lo, hi, ok, err := p.epochBounds(ctx, args[4].(time.Time), args[5].(time.Time))
	if err != nil || !ok {
		return err
	}
	args[2], args[3] = lo, hi
	rows, err := p.db().QueryContext(ctx, `
WITH v(account, service, endpoint, level) AS (VALUES `+strings.Join(values, ", ")+`
), eps AS (SELECT account, service, endpoint FROM v), pts AS (`+microPoints+`
), d AS (
  SELECT account, service, endpoint, t, r - lag(r) OVER w AS dr, er - lag(er) OVER w AS de, ns - lag(ns) OVER w AS dn
  FROM pts WINDOW w AS (PARTITION BY account, service, endpoint, instance_id, started ORDER BY t)
), c AS (
  SELECT account, service, endpoint, t, sum(dn) / sum(dr) / 1e6 AS ms FROM d WHERE dr >= 0 AND de >= 0 AND dn >= 0
  GROUP BY ALL HAVING sum(dr) > 0
), s AS (
  SELECT c.account, c.service, c.endpoint, c.t,
    c.ms >= `+fmt.Sprint(api.MicroSlowFactor)+` * v.level AND c.ms >= v.level + `+fmt.Sprint(api.MicroSlowMarginMs)+` AS slow
  FROM c JOIN v USING (account, service, endpoint)
), b AS (
  SELECT account, service, endpoint, max(t) FILTER (WHERE slow) AS last_slow FROM s GROUP BY ALL
), f AS (
  SELECT b.account, b.service, b.endpoint, b.last_slow, max(s.t) FILTER (WHERE NOT s.slow AND s.t < b.last_slow) AS last_fast
  FROM b JOIN s USING (account, service, endpoint) GROUP BY ALL
)
SELECT f.account, f.service, f.endpoint, min(s.t)
FROM f JOIN s USING (account, service, endpoint)
WHERE s.slow AND s.t <= f.last_slow AND (f.last_fast IS NULL OR s.t > f.last_fast) GROUP BY ALL`, args...)
	if err != nil {
		return fmt.Errorf("micro slow since: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var acc, svc, ep string
		var t time.Time
		if err := rows.Scan(&acc, &svc, &ep, &t); err != nil {
			return err
		}
		if x := eps.byKey[microKey(acc, svc, ep)]; x != nil {
			t = t.UTC()
			x.fact.SlowSince = &t
		}
	}
	return rows.Err()
}

// goneAsked reads the instances of the accounts asked in the epoch that
// answered $SRV.STATS within the api.MicroMissingWindow before it and not
// in it: each with its last answer, the account's next STATS request after
// it, the service's instances in that last answer, and the name of the
// connection that held its $SRV subscriptions in the newest subscription
// collection within two hours of the epoch that lists it.
func (p *Provider) goneAsked(ctx context.Context, e epochInfo, account string) ([]api.MicroGoneFact, error) {
	lo, _, ok, err := p.epochBounds(ctx, e.startedAt.Add(-api.MicroMissingWindow), e.startedAt.Add(time.Nanosecond))
	if err != nil || !ok {
		return nil, err
	}
	slo, _, _, err := p.epochBounds(ctx, e.startedAt.Add(-2*api.MicroMissingWindow), e.startedAt.Add(time.Nanosecond))
	if err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH asked AS (
  SELECT DISTINCT account FROM micro_requests WHERE epoch = $1 AND request = 'STATS' AND ($2 = '' OR account = $2)
), reqs AS (
  SELECT DISTINCT r.account, r.epoch, ep.started_at FROM micro_requests r JOIN epochs ep USING (epoch)
  WHERE r.epoch BETWEEN $3 AND $1 AND r.request = 'STATS' AND r.account IN (SELECT account FROM asked)
), st AS (
  SELECT epoch, account, service, instance_id FROM micro_stats WHERE epoch BETWEEN $3 AND $1 AND account IN (SELECT account FROM asked)
), last AS (
  SELECT account, service, instance_id, max(epoch) AS epoch FROM st GROUP BY ALL HAVING max(epoch) < $1
), cols AS (
  SELECT x.epoch FROM epochs x WHERE x.epoch BETWEEN $4 AND $1
    AND EXISTS (SELECT 1 FROM epoch_endpoints q WHERE q.epoch = x.epoch AND q.endpoint = 'SUBSZ' AND q.state = 'ok')
), i AS (`+srvInstances+`
), held AS (
  SELECT i.account, i.service, i.id, arg_max(c.name, i.epoch) AS name
  FROM i JOIN last l ON l.account = i.account AND l.service = i.service AND l.instance_id = i.id
  LEFT JOIN connections c ON c.epoch = i.epoch AND c.server_id = i.server_id AND c.cid = i.cid GROUP BY ALL
)
SELECT l.account, l.service, l.instance_id, le.started_at,
  (SELECT min(q.started_at) FROM reqs q WHERE q.account = l.account AND q.epoch > l.epoch),
  coalesce(h.name, ''), (SELECT count(*) FROM st o WHERE o.epoch = l.epoch AND o.account = l.account AND o.service = l.service)
FROM last l JOIN epochs le ON le.epoch = l.epoch
LEFT JOIN held h ON h.account = l.account AND h.service = l.service AND h.id = l.instance_id
ORDER BY 1, 2, 3`, e.epoch, account, lo, slo)
	if err != nil {
		return nil, fmt.Errorf("gone micro instances asked: %w", err)
	}
	defer rows.Close()
	var out []api.MicroGoneFact
	for rows.Next() {
		var g api.MicroGoneFact
		var since sql.NullTime
		if err := rows.Scan(&g.Account, &g.Service, &g.ID, &g.LastSeen, &since, &g.Connection, &g.InstancesThen); err != nil {
			return nil, err
		}
		g.LastSeen, g.Since = g.LastSeen.UTC(), since.Time.UTC()
		out = append(out, g)
	}
	return out, rows.Err()
}
