package storeprov

import (
	"context"
	"database/sql"
	"fmt"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListAccounts lists the accounts the newest epoch of the range reported,
// ordered by account. Connections add up each server's share from STATZ.
// Streams and consumers are counted from the JSZ detail; without detail,
// a JSZ count of zero is zero and any other count is unknown, since the
// servers count each replica.
// A range without an epoch lists the accounts seen up to its end, every
// count unset, so that an account picker is not empty.
func (p *Provider) ListAccounts(ctx context.Context, req *v1.ListAccountsRequest) (*v1.ListAccountsResponse, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	if !ok {
		return p.accountsSeen(ctx, req.GetRange())
	}
	rows, err := p.db().QueryContext(ctx, `
WITH st AS (
    SELECT account, sum(conns)::BIGINT AS conns, max(js_streams) AS js_streams, max(js_consumers) AS js_consumers
    FROM account_stats WHERE epoch = $1 GROUP BY account
), ss AS (
    SELECT account, count(DISTINCT stream) AS n FROM stream_stats WHERE epoch = $1 GROUP BY account
), cs AS (
    SELECT account, count(DISTINCT (stream, consumer)) AS n FROM consumer_stats WHERE epoch = $1 GROUP BY account
)
SELECT a.account, a.account_name, a.is_system, st.conns, st.js_streams, ss.n, st.js_consumers, cs.n
FROM accounts a LEFT JOIN st USING (account) LEFT JOIN ss USING (account) LEFT JOIN cs USING (account)
WHERE a.first_epoch <= $1 AND a.last_epoch >= $1
ORDER BY a.account`, e.epoch)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	defer rows.Close()
	res := &v1.ListAccountsResponse{}
	for rows.Next() {
		var (
			a                         v1.Account
			name                      sql.NullString
			conns, streams, consumers sql.NullInt64
			jsStreams, jsConsumers    sql.NullInt64
		)
		if err := rows.Scan(&a.Account, &name, &a.IsSystem, &conns, &jsStreams, &streams, &jsConsumers, &consumers); err != nil {
			return nil, err
		}
		if name.String != a.Account {
			a.AccountName = name.String
		}
		if conns.Valid {
			a.Connections = ptr(int32(conns.Int64))
		}
		a.Streams, a.Consumers = jsCount(streams, jsStreams), jsCount(consumers, jsConsumers)
		res.Accounts = append(res.Accounts, &a)
	}
	return res, rows.Err()
}

// jsCount is the count from the JSZ detail when there is any, else zero
// when the servers report zero, else unknown.
func jsCount(detail, reported sql.NullInt64) *int32 {
	switch {
	case detail.Valid:
		return ptr(int32(detail.Int64))
	case reported.Valid && reported.Int64 == 0:
		return ptr(int32(0))
	}
	return nil
}

// accountsSeen lists the accounts some epoch before the range's end
// reported, without counts.
func (p *Provider) accountsSeen(ctx context.Context, r *v1.TimeRange) (*v1.ListAccountsResponse, error) {
	_, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
SELECT account, account_name, is_system FROM accounts
WHERE first_epoch <= (SELECT max(epoch) FROM epochs WHERE started_at < ?)
ORDER BY account`, end)
	if err != nil {
		return nil, fmt.Errorf("accounts seen: %w", err)
	}
	defer rows.Close()
	res := &v1.ListAccountsResponse{}
	for rows.Next() {
		var a v1.Account
		var name sql.NullString
		if err := rows.Scan(&a.Account, &name, &a.IsSystem); err != nil {
			return nil, err
		}
		if name.String != a.Account {
			a.AccountName = name.String
		}
		res.Accounts = append(res.Accounts, &a)
	}
	return res, rows.Err()
}
