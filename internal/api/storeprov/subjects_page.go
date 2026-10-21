package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Subjects reads the range's newest epoch: its streams with their
// configuration, consumers and stored rate, the JetStream API rates of the
// accounts that use JetStream, and the newest subscription collection at
// or before the epoch within api.SubscriptionsMaxAge, grouped in SQL by
// api.GroupSubscriptions' rules. The number of queries is fixed; no
// subscription row is read into Go.
func (p *Provider) Subjects(ctx context.Context, req *v1.GetSubjectsRequest) (*api.SubjectsFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.SubjectsFacts{}, err
	}
	f := &api.SubjectsFacts{Epoch: e.epoch, EpochAt: e.startedAt}
	acc := req.GetAccount()
	if err := p.db().QueryRowContext(ctx, `SELECT coalesce(min(account) FILTER (WHERE is_system), '') FROM accounts`).Scan(&f.SystemAccount); err != nil {
		return nil, fmt.Errorf("system account: %w", err)
	}
	if f.Streams, err = p.subjectStreams(ctx, e.epoch, req.GetRange(), acc); err != nil {
		return nil, err
	}
	if f.APIs, err = p.jetStreamAPIs(ctx, e.epoch, req.GetRange(), acc); err != nil {
		return nil, err
	}
	c, ok, err := p.subsCollection(ctx, e)
	if err != nil || !ok {
		return f, err
	}
	sf := &api.SubscriptionFacts{At: c.startedAt, ServerSubscriptions: map[string]int64{}, Unlisted: map[string]int64{}}
	if sf.Servers, err = p.subszServers(ctx, c.epoch); err != nil {
		return nil, err
	}
	if err := p.subscriptionGroups(ctx, sf, c.epoch, e.epoch, acc); err != nil {
		return nil, err
	}
	f.Subscriptions = sf
	return f, nil
}

// MatchSubject reads what Subjects reads for the tester: the streams, and
// the subscription collection's client subscriptions grouped by account,
// subject and queue (api.CoreGroups), only those whose subject may match
// the literal: the literal itself, or a subject with a wildcard whose
// first token is a wildcard or the literal's. FinishMatch decides.
func (p *Provider) MatchSubject(ctx context.Context, req *v1.MatchSubjectRequest) (*api.MatchFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.MatchFacts{}, err
	}
	f := &api.MatchFacts{Epoch: e.epoch, EpochAt: e.startedAt}
	if f.Streams, err = p.subjectStreams(ctx, e.epoch, req.GetRange(), req.GetAccount()); err != nil {
		return nil, err
	}
	c, ok, err := p.subsCollection(ctx, e)
	if err != nil || !ok {
		return f, err
	}
	f.SubscriptionsAt = &c.startedAt
	rows, err := p.db().QueryContext(ctx, `
WITH s AS (
  SELECT s.server_id, s.account, s.subject, coalesce(s.queue, '') AS queue, s.cid, c.name
  FROM subscriptions s JOIN connections c ON c.epoch = s.epoch AND c.server_id = s.server_id AND c.cid = s.cid
  WHERE s.epoch = $1 AND ($2 = '' OR s.account = $2)
    AND (s.subject = $3 OR ((split_part(s.subject, '.', 1) IN ('*', '>') OR split_part(s.subject, '.', 1) = split_part($3, '.', 1))
      AND (s.subject LIKE '%*%' OR s.subject LIKE '%>'))))
SELECT account, subject, queue, count(DISTINCT (server_id, cid))::INTEGER,
  coalesce(list(DISTINCT name ORDER BY name) FILTER (WHERE coalesce(name, '') <> ''), [])
FROM s GROUP BY account, subject, queue ORDER BY account, subject, queue`, c.epoch, req.GetAccount(), req.GetSubject())
	if err != nil {
		return nil, fmt.Errorf("subject subscribers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g api.SubscriptionGroup
		var names []any
		if err := rows.Scan(&g.Account, &g.Subject, &g.Queue, &g.Connections, &names); err != nil {
			return nil, err
		}
		g.Names = strs(names)
		g.NameCount = int32(len(g.Names))
		f.Core = append(f.Core, g)
	}
	return f, rows.Err()
}

// subjectStreams reads the streams of the epoch with ListStreams' messages
// (the leader's, else the largest), the configuration of the leader (else
// the first reporting server by name, as the Capacity page), their
// consumers as consumer_stats lists them with the configuration of the
// same choice, and the stored rate.
func (p *Provider) subjectStreams(ctx context.Context, epoch int64, r *v1.TimeRange, account string) ([]api.SubjectStreamFact, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH st AS (
  SELECT account, stream, coalesce(max(messages) FILTER (WHERE is_leader), max(messages)) AS messages
  FROM stream_stats WHERE epoch = $1 AND ($2 = '' OR account = $2) GROUP BY ALL
), cf AS (
  SELECT c.account, c.stream, c.retention, c.mirror, c.subjects, c.sources
  FROM stream_config c JOIN servers s USING (server_id)
  LEFT JOIN stream_stats ss ON ss.epoch = $1 AND ss.server_id = c.server_id AND ss.account = c.account AND ss.stream = c.stream
  WHERE c.first_epoch <= $1 AND c.last_epoch >= $1 AND ($2 = '' OR c.account = $2)
  QUALIFY row_number() OVER (PARTITION BY c.account, c.stream ORDER BY coalesce(ss.is_leader, false) DESC, s.server_name) = 1
)
SELECT st.account, st.stream, coalesce(cf.retention, ''), coalesce(cf.mirror, ''), coalesce(cf.subjects, []), coalesce(cf.sources, []), st.messages
FROM st LEFT JOIN cf USING (account, stream) ORDER BY st.account, st.stream`, epoch, account)
	if err != nil {
		return nil, fmt.Errorf("subject streams: %w", err)
	}
	defer rows.Close()
	var out []api.SubjectStreamFact
	index := map[string]int{}
	for rows.Next() {
		var s api.SubjectStreamFact
		var subjects, sources []any
		var msgs sql.NullInt64
		if err := rows.Scan(&s.Account, &s.Stream, &s.Retention, &s.Mirror, &subjects, &sources, &msgs); err != nil {
			return nil, err
		}
		s.Subjects, s.Sources, s.Messages = strs(subjects), strs(sources), int64Ptr(msgs)
		index[s.Account+"/"+s.Stream] = len(out)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cons, err := p.db().QueryContext(ctx, `
SELECT x.account, x.stream, x.consumer, coalesce(cc.durable_name, '') <> '', coalesce(cc.filter_subjects, []), coalesce(cc.deliver_subject, '')
FROM consumer_stats x JOIN servers s USING (server_id)
LEFT JOIN consumer_config cc ON cc.server_id = x.server_id AND cc.account = x.account AND cc.stream = x.stream
  AND cc.consumer = x.consumer AND x.epoch BETWEEN cc.first_epoch AND cc.last_epoch
WHERE x.epoch = $1 AND ($2 = '' OR x.account = $2)
QUALIFY row_number() OVER (PARTITION BY x.account, x.stream, x.consumer
  ORDER BY coalesce(x.is_leader, false) DESC, cc.consumer IS NULL, s.server_name) = 1
ORDER BY x.account, x.stream, x.consumer`, epoch, account)
	if err != nil {
		return nil, fmt.Errorf("subject consumers: %w", err)
	}
	defer cons.Close()
	for cons.Next() {
		var acc, stream string
		var c api.SubjectConsumerFact
		var filters []any
		if err := cons.Scan(&acc, &stream, &c.Name, &c.Durable, &filters, &c.DeliverSubject); err != nil {
			return nil, err
		}
		c.Filters = strs(filters)
		if i, ok := index[acc+"/"+stream]; ok {
			out[i].Consumers = append(out[i].Consumers, c)
		}
	}
	if err := cons.Err(); err != nil {
		return nil, err
	}
	rates, err := p.storedRates(ctx, r, account)
	if err != nil {
		return nil, err
	}
	for i, s := range out {
		if v, ok := rates[s.Account+"/"+s.Stream]; ok {
			out[i].StoredPerSecond = ptr(v)
		}
	}
	return out, nil
}

// storedRates reads each stream's stored messages per second over the
// api.RateSpan before the range's end: the increase of the leader's last
// sequence per second between the first and the last leader row of the
// newest run of one leading server and one creation time, as
// deliveredRates reads a consumer's delivered sequence. A stream with
// fewer than two such rows has none.
func (p *Provider) storedRates(ctx context.Context, r *v1.TimeRange, account string) (map[string]float64, error) {
	_, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
WITH lead AS (
  SELECT x.account, x.stream, x.server_id, x.created, e.started_at AS t, x.last_seq AS v
  FROM stream_stats x JOIN epochs e USING (epoch)
  WHERE x.is_leader AND x.last_seq IS NOT NULL AND e.started_at >= ? AND e.started_at < ? AND (? = '' OR x.account = ?)
  QUALIFY row_number() OVER (PARTITION BY x.account, x.stream, e.epoch ORDER BY x.last_seq DESC, x.server_id) = 1),
newest AS (
  SELECT account, stream, arg_max(server_id, t) AS server_id, arg_max(created, t) AS created FROM lead GROUP BY ALL)
SELECT l.account, l.stream,
  (arg_max(l.v, l.t) - arg_min(l.v, l.t)) * 1000.0 / date_diff('millisecond', min(l.t), max(l.t))
FROM lead l JOIN newest n ON n.account = l.account AND n.stream = l.stream AND n.server_id = l.server_id
  AND n.created IS NOT DISTINCT FROM l.created
GROUP BY ALL HAVING count(*) >= 2 AND max(l.t) > min(l.t)`, end.Add(-api.RateSpan), end, account, account)
	if err != nil {
		return nil, fmt.Errorf("stored rates: %w", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var acc, st string
		var v float64
		if err := rows.Scan(&acc, &st, &v); err != nil {
			return nil, err
		}
		if v >= 0 {
			out[acc+"/"+st] = v
		}
	}
	return out, rows.Err()
}

// jetStreamAPIs reads the accounts with JetStream use in the epoch (a
// stream, or JetStream API requests) and their API requests per second
// and error share over the api.RateSpan before the range's end: per
// server, the increase between its first and last report there (a server
// ID is one process, so a restart starts over; a counter that fell is
// left out), summed over the servers.
func (p *Provider) jetStreamAPIs(ctx context.Context, epoch int64, r *v1.TimeRange, account string) ([]api.JetStreamAPIFact, error) {
	_, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
WITH used AS (
  SELECT DISTINCT account FROM account_stats
  WHERE epoch = $1 AND (coalesce(js_streams, 0) > 0 OR coalesce(js_api_total, 0) > 0) AND ($4 = '' OR account = $4)
  UNION SELECT DISTINCT account FROM stream_stats WHERE epoch = $1 AND ($4 = '' OR account = $4)
), per AS (
  SELECT a.account, a.server_id,
    arg_max(a.js_api_total, e.started_at) - arg_min(a.js_api_total, e.started_at) AS total,
    arg_max(a.js_api_errors, e.started_at) - arg_min(a.js_api_errors, e.started_at) AS errors,
    date_diff('millisecond', min(e.started_at), max(e.started_at)) AS ms
  FROM account_stats a JOIN epochs e USING (epoch)
  WHERE e.started_at >= $2 AND e.started_at < $3 AND a.js_api_total IS NOT NULL AND a.account IN (SELECT account FROM used)
  GROUP BY ALL HAVING count(*) >= 2 AND max(e.started_at) > min(e.started_at)
), ok AS (SELECT * FROM per WHERE total >= 0)
SELECT u.account, sum(o.total * 1000.0 / o.ms), sum(o.total)::DOUBLE, sum(greatest(coalesce(o.errors, 0), 0))::DOUBLE
FROM used u LEFT JOIN ok o USING (account) GROUP BY u.account ORDER BY u.account`, epoch, end.Add(-api.RateSpan), end, account)
	if err != nil {
		return nil, fmt.Errorf("jetstream api: %w", err)
	}
	defer rows.Close()
	var out []api.JetStreamAPIFact
	for rows.Next() {
		var a api.JetStreamAPIFact
		var rate, total, errs sql.NullFloat64
		if err := rows.Scan(&a.Account, &rate, &total, &errs); err != nil {
			return nil, err
		}
		if rate.Valid {
			a.PerSecond = ptr(rate.Float64)
			if total.Float64 > 0 {
				a.ErrorShare = ptr(errs.Float64 / total.Float64)
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// subsCollection is the newest epoch at or before e, within
// api.SubscriptionsMaxAge, that asked SUBSZ; false when there is none.
func (p *Provider) subsCollection(ctx context.Context, e epochInfo) (epochInfo, bool, error) {
	var c epochInfo
	err := p.db().QueryRowContext(ctx, `
SELECT x.epoch, x.started_at FROM epochs x
WHERE x.epoch <= ? AND x.started_at >= ? AND EXISTS (SELECT 1 FROM epoch_endpoints p WHERE p.epoch = x.epoch AND p.endpoint = 'SUBSZ')
ORDER BY x.epoch DESC LIMIT 1`, e.epoch, e.startedAt.Add(-api.SubscriptionsMaxAge)).Scan(&c.epoch, &c.startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, fmt.Errorf("subscription collection: %w", err)
	}
	c.startedAt = c.startedAt.UTC()
	return c, true, nil
}

// subszServers reads the servers the collection asked: each with a SUBSZ
// outcome, answered when subscription_lists has its row, and every server
// the epoch missed, unanswered.
func (p *Provider) subszServers(ctx context.Context, epoch int64) ([]api.SubszServerFact, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH asked AS (
  SELECT s.server_name AS name, max(l.total) AS total, max(l.listed) AS listed
  FROM epoch_endpoints x JOIN servers s USING (server_id)
  LEFT JOIN subscription_lists l ON l.epoch = x.epoch AND l.server_id = x.server_id
  WHERE x.epoch = $1 AND x.endpoint = 'SUBSZ' GROUP BY s.server_name
)
SELECT name, total, listed FROM asked
UNION ALL
SELECT m, NULL, NULL FROM (SELECT unnest(missing_names) AS m FROM epochs WHERE epoch = $1) WHERE m NOT IN (SELECT name FROM asked)
ORDER BY 1`, epoch)
	if err != nil {
		return nil, fmt.Errorf("subsz servers: %w", err)
	}
	defer rows.Close()
	var out []api.SubszServerFact
	for rows.Next() {
		var s api.SubszServerFact
		var total, listed sql.NullInt64
		if err := rows.Scan(&s.Name, &total, &listed); err != nil {
			return nil, err
		}
		s.Answered, s.Total, s.Listed = total.Valid, int64Ptr(total), int64Ptr(listed)
		out = append(out, s)
	}
	return out, rows.Err()
}

// subscriptionGroups groups the collection's subscriptions in SQL by
// api.GroupSubscriptions' rules. A subscription joins its connection on
// the collection's epoch, server and client ID, never on account (SUBSZ
// names the global account $G where CONNZ leaves it empty). One whose
// connection is missing is the server's own when that server's CONNZ
// answered in the epoch and listed every connection (connz_returned not
// below connz_total), else unlisted. Of the clients' subscriptions, those
// on a push consumer's deliver subject in the page's epoch go to Deliver
// with every name; the others, each _INBOX. subject folded, to Groups,
// with at most api.MaxClientNames names and the count of all.
func (p *Provider) subscriptionGroups(ctx context.Context, sf *api.SubscriptionFacts, collection, epoch int64, account string) error {
	rows, err := p.db().QueryContext(ctx, `
WITH s AS (
  SELECT * FROM subscriptions WHERE epoch = $1 AND ($3 = '' OR account = $3)
), connz AS (
  SELECT x.server_id, coalesce(any_value(st.connz_total IS NOT NULL AND st.connz_returned >= st.connz_total), false) AS complete
  FROM epoch_endpoints x LEFT JOIN server_stats st ON st.epoch = x.epoch AND st.server_id = x.server_id
  WHERE x.epoch = $1 AND x.endpoint = 'CONNZ' AND x.state = 'ok' GROUP BY x.server_id
), deliver AS (
  SELECT DISTINCT account, deliver_subject AS subject FROM consumer_config
  WHERE $2 BETWEEN first_epoch AND last_epoch AND coalesce(deliver_subject, '') <> ''
), j AS (
  SELECT s.server_id, s.account, s.subject, coalesce(s.queue, '') AS queue, s.cid, s.subs, c.name,
    c.cid IS NOT NULL AS client, coalesce(z.complete, false) AS complete, d.subject IS NOT NULL AS is_deliver
  FROM s LEFT JOIN connections c ON c.epoch = s.epoch AND c.server_id = s.server_id AND c.cid = s.cid
  LEFT JOIN connz z ON z.server_id = s.server_id
  LEFT JOIN deliver d ON d.account = s.account AND d.subject = s.subject
), g AS (
  SELECT CASE WHEN is_deliver THEN 'deliver' ELSE 'group' END AS part, account,
    CASE WHEN NOT is_deliver AND starts_with(subject, '`+api.InboxPrefix+`') THEN '`+api.InboxPattern+`' ELSE subject END AS subject,
    queue, server_id, cid, name
  FROM j WHERE client
)
SELECT 'left', account, '', '',
  coalesce(sum(subs) FILTER (WHERE complete), 0)::BIGINT, coalesce(sum(subs) FILTER (WHERE NOT complete), 0)::BIGINT,
  []::VARCHAR[], 0::BIGINT
FROM j WHERE NOT client GROUP BY account
UNION ALL
SELECT part, account, subject, CASE WHEN count(DISTINCT queue) = 1 THEN min(queue) ELSE '' END,
  count(DISTINCT (server_id, cid))::BIGINT, 0::BIGINT,
  CASE WHEN part = 'deliver' THEN coalesce(list(DISTINCT name ORDER BY name) FILTER (WHERE coalesce(name, '') <> ''), [])
    ELSE coalesce(list(DISTINCT name ORDER BY name) FILTER (WHERE coalesce(name, '') <> ''), [])[1:`+fmt.Sprint(api.MaxClientNames)+`] END,
  count(DISTINCT name) FILTER (WHERE coalesce(name, '') <> '')
FROM g GROUP BY part, account, subject
ORDER BY 1, 2, 3, 4`, collection, epoch, account)
	if err != nil {
		return fmt.Errorf("subscription groups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var part string
		var g api.SubscriptionGroup
		var a, b, n int64
		var names []any
		if err := rows.Scan(&part, &g.Account, &g.Subject, &g.Queue, &a, &b, &names, &n); err != nil {
			return err
		}
		switch part {
		case "left":
			if a > 0 {
				sf.ServerSubscriptions[g.Account] += a
			}
			if b > 0 {
				sf.Unlisted[g.Account] += b
			}
		case "deliver":
			g.Connections, g.Names, g.NameCount = int32(a), strs(names), int32(n)
			sf.Deliver = append(sf.Deliver, g)
		default:
			g.Connections, g.Names, g.NameCount = int32(a), strs(names), int32(n)
			sf.Groups = append(sf.Groups, g)
		}
	}
	return rows.Err()
}
