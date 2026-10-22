package storeprov

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// srvInstances selects, from the subscriptions of the epochs the query
// names cols (a relation with an epoch column), the micro service
// instances: one row per epoch, account, service and instance ID of a
// $SRV.<PING|INFO|STATS>.<name>.<id> subscription, with a server and
// client ID that hold it (of the lowest server ID).
const srvInstances = `
  SELECT s.epoch, s.account, split_part(s.subject, '.', 3) AS service, split_part(s.subject, '.', 4) AS id,
    min(s.server_id) AS server_id, arg_min(s.cid, s.server_id) AS cid
  FROM subscriptions s
  WHERE s.epoch IN (SELECT epoch FROM cols) AND starts_with(s.subject, '` + api.SrvPrefix + `')
    AND len(string_split(s.subject, '.')) = 4 AND split_part(s.subject, '.', 2) IN ('PING', 'INFO', 'STATS')
    AND split_part(s.subject, '.', 3) NOT IN ('', '*', '>') AND split_part(s.subject, '.', 4) NOT IN ('', '*', '>')
    AND ($2 = '' OR s.account = $2)
  GROUP BY ALL`

// MicroServices reads the range's newest epoch's micro services: the
// accounts asked in it, the services that answered its $SRV.STATS with
// their newest $SRV.INFO and their endpoints' rates, levels and slow runs,
// the instances of those accounts gone in the api.MicroMissingWindow
// before it; and from the newest subscription collection at or before the
// epoch the services seen there (api.SeenMicro's rules, in SQL), with the
// instances of the other accounts seen in the window's collections and
// not in it. The number of queries is fixed; no stats row is read into Go.
func (p *Provider) MicroServices(ctx context.Context, req *v1.GetMicroServicesRequest) (*api.MicroFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.MicroFacts{}, err
	}
	acc := req.GetAccount()
	f := &api.MicroFacts{Epoch: e.epoch, EpochAt: e.startedAt}
	if f.SystemAccount, err = p.systemAccount(ctx); err != nil {
		return nil, err
	}
	if f.Accounts, err = p.microAccounts(ctx, e, acc); err != nil {
		return nil, err
	}
	if f.Answered, err = p.microAnswered(ctx, e, req.GetRange(), acc); err != nil {
		return nil, err
	}
	if f.Gone, err = p.goneAsked(ctx, e, acc); err != nil {
		return nil, err
	}
	c, ok, err := p.subsCollection(ctx, e)
	if err != nil || !ok {
		return f, err
	}
	f.SubscriptionsAt = &c.startedAt
	if f.Seen, err = p.seenMicro(ctx, c.epoch, acc); err != nil {
		return nil, err
	}
	gone, err := p.goneSeen(ctx, e, c.epoch, acc)
	if err != nil {
		return nil, err
	}
	f.Gone = append(f.Gone, gone...)
	return f, nil
}

// seenMicro reads the services of a subscription collection: each
// instance with the connection that holds its $SRV subscriptions, named
// when the collection's CONNZ lists it (joined on server and client ID).
func (p *Provider) seenMicro(ctx context.Context, collection int64, account string) ([]api.MicroServiceFact, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH cols AS (SELECT $1::BIGINT AS epoch), i AS (`+srvInstances+`)
SELECT i.account, i.service, i.id, coalesce(c.name, ''), CASE WHEN c.cid IS NULL THEN '' ELSE coalesce(sv.server_name, '') END
FROM i LEFT JOIN connections c ON c.epoch = i.epoch AND c.server_id = i.server_id AND c.cid = i.cid
LEFT JOIN servers sv ON sv.server_id = i.server_id
ORDER BY i.account, i.service, i.id`, collection, account)
	if err != nil {
		return nil, fmt.Errorf("seen micro services: %w", err)
	}
	defer rows.Close()
	var out []api.MicroServiceFact
	for rows.Next() {
		var acc, svc string
		var in api.MicroInstanceFact
		if err := rows.Scan(&acc, &svc, &in.ID, &in.Connection, &in.Server); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Account != acc || out[n-1].Name != svc {
			out = append(out, api.MicroServiceFact{Account: acc, Name: svc})
		}
		out[len(out)-1].Instances = append(out[len(out)-1].Instances, in)
	}
	return out, rows.Err()
}

// goneSeen reads the instances of the accounts not asked in the epoch
// seen in a subscription collection of the api.MicroMissingWindow before
// it and not in the newest one: each
// with its last collection, the next collection after it, its connection
// then and the service's instances then. An instance last held on a
// server whose list in the newest collection was cut at the limit or did
// not come is left out: its absence there says nothing.
func (p *Provider) goneSeen(ctx context.Context, e epochInfo, collection int64, account string) ([]api.MicroGoneFact, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH cols AS (
  SELECT x.epoch, x.started_at FROM epochs x
  WHERE x.epoch <= $1 AND x.started_at >= $3
    AND EXISTS (SELECT 1 FROM epoch_endpoints q WHERE q.epoch = x.epoch AND q.endpoint = 'SUBSZ' AND q.state = 'ok')
), i AS (`+srvInstances+`
), last AS (
  SELECT account, service, id, max(epoch) AS epoch FROM i GROUP BY ALL HAVING max(epoch) < $1
), complete AS (
  SELECT server_id FROM subscription_lists WHERE epoch = $1 AND listed >= total
)
SELECT l.account, l.service, l.id, c.started_at,
  (SELECT min(n.started_at) FROM cols n WHERE n.epoch > l.epoch AND n.epoch <= $1),
  coalesce(cn.name, ''), (SELECT count(*) FROM i o WHERE o.epoch = l.epoch AND o.account = l.account AND o.service = l.service)
FROM last l JOIN cols c ON c.epoch = l.epoch
JOIN i ON i.epoch = l.epoch AND i.account = l.account AND i.service = l.service AND i.id = l.id
LEFT JOIN connections cn ON cn.epoch = i.epoch AND cn.server_id = i.server_id AND cn.cid = i.cid
WHERE i.server_id IN (SELECT server_id FROM complete)
  AND l.account NOT IN (SELECT account FROM micro_requests WHERE epoch = $4 AND request = 'STATS')
ORDER BY l.account, l.service, l.id`, collection, account, e.startedAt.Add(-api.MicroMissingWindow), e.epoch)
	if err != nil {
		return nil, fmt.Errorf("gone micro instances: %w", err)
	}
	defer rows.Close()
	var out []api.MicroGoneFact
	for rows.Next() {
		var g api.MicroGoneFact
		var since sql.NullTime
		if err := rows.Scan(&g.Account, &g.Service, &g.ID, &g.LastSeen, &since, &g.Connection, &g.InstancesThen); err != nil {
			return nil, err
		}
		g.LastSeen = g.LastSeen.UTC()
		g.Since = since.Time.UTC()
		out = append(out, g)
	}
	return out, rows.Err()
}
