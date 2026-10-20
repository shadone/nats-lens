package storeprov

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// AccountsAndAuth reads the range's newest epoch: its accounts with their
// statistics and newest lookup, the failed logins of the 24 hours before
// it grouped in SQL, whether event capture had gaps then, and the epoch's
// listener certificates and user JWTs.
func (p *Provider) AccountsAndAuth(ctx context.Context, req *v1.GetAccountsAndAuthRequest) (*api.AccountsFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.AccountsFacts{}, err
	}
	f := &api.AccountsFacts{Epoch: e.epoch, EpochAt: e.startedAt}
	steps := []func(context.Context, *api.AccountsFacts) error{
		p.accountFacts, p.failedLogins, p.captureGaps, p.certificates, p.userJWTs,
	}
	for _, step := range steps {
		if err := step(ctx, f); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// accountFacts reads the accounts ListAccounts lists for the epoch:
// connections, leaf nodes and subscriptions summed over the servers'
// STATZ shares, JetStream use the largest any server reports (each
// reports the account total), distinct streams, and the newest lookup at
// or before the epoch within AccountDetailsMaxAge.
func (p *Provider) accountFacts(ctx context.Context, f *api.AccountsFacts) error {
	rows, err := p.db().QueryContext(ctx, `
WITH st AS (
    SELECT account, sum(conns)::BIGINT AS conns, sum(leafnodes)::BIGINT AS leafs, sum(subscriptions)::BIGINT AS subs,
        max(js_storage) AS js_storage, max(js_memory) AS js_memory, max(js_streams) AS js_streams
    FROM account_stats WHERE epoch = $1 GROUP BY account
), ss AS (
    SELECT account, count(DISTINCT stream) AS n FROM stream_stats WHERE epoch = $1 GROUP BY account
), d AS (
    SELECT * FROM account_details WHERE epoch <= $1 AND looked_up_at <= $2 AND looked_up_at >= $3
    QUALIFY row_number() OVER (PARTITION BY account ORDER BY epoch DESC, looked_up_at DESC) = 1
)
SELECT a.account, coalesce(nullif(a.account_name, a.account), nullif(d.account_name, a.account), ''),
    a.is_system OR coalesce(d.is_system, false), st.conns, st.leafs, st.subs, st.js_storage, st.js_memory, ss.n, st.js_streams,
    d.looked_up_at, d.has_jwt, d.expired, d.imports, d.exports, d.jwt_expires, d.limit_connections, d.limit_js_disk, d.limit_js_memory
FROM accounts a LEFT JOIN st USING (account) LEFT JOIN ss USING (account) LEFT JOIN d USING (account)
WHERE a.first_epoch <= $1 AND a.last_epoch >= $1
ORDER BY a.account`, f.Epoch, f.EpochAt, f.EpochAt.Add(-api.AccountDetailsMaxAge))
	if err != nil {
		return fmt.Errorf("accounts and auth accounts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			a                                  api.AccountFact
			conns, leafs, subs, disk, mem      sql.NullInt64
			streams, jsStreams                 sql.NullInt64
			at, expires                        sql.NullTime
			hasJWT, expired                    sql.NullBool
			imports, exports, connLimit, limit sql.NullInt64
			memLimit                           sql.NullInt64
		)
		if err := rows.Scan(&a.Account, &a.Name, &a.IsSystem, &conns, &leafs, &subs, &disk, &mem, &streams, &jsStreams,
			&at, &hasJWT, &expired, &imports, &exports, &expires, &connLimit, &limit, &memLimit); err != nil {
			return err
		}
		a.Connections, a.Leafnodes = int32Of(conns), int32Of(leafs)
		a.Subscriptions, a.JSStorage, a.JSMemory = int64Ptr(subs), int64Ptr(disk), int64Ptr(mem)
		a.Streams = jsCount(streams, jsStreams)
		if at.Valid {
			d := &api.AccountDetails{At: at.Time.UTC(), HasJWT: hasJWT.Bool, Expired: expired.Bool,
				Imports: int32(imports.Int64), Exports: int32(exports.Int64), LimitConnections: int64Ptr(connLimit), LimitJSDisk: int64Ptr(limit),
				LimitJSMemory: int64Ptr(memLimit)}
			if expires.Valid {
				d.JWTExpires = ptr(expires.Time.UTC())
			}
			a.Details = d
		}
		f.Accounts = append(f.Accounts, a)
	}
	return rows.Err()
}

func int32Of(v sql.NullInt64) *int32 {
	if !v.Valid {
		return nil
	}
	return ptr(int32(v.Int64))
}

// failedLogins reads the authentication errors of the window, from 24
// hours before the epoch (or the first collection, when later) to the
// epoch, grouped by user, address and reason: the MaxFailedLoginGroups
// most frequent, with the count of every group and of every event.
func (p *Provider) failedLogins(ctx context.Context, f *api.AccountsFacts) error {
	from := f.EpochAt.Add(-api.FailedLoginWindow)
	var first sql.NullTime
	if err := p.db().QueryRowContext(ctx, `SELECT min(started_at) FROM epochs`).Scan(&first); err != nil {
		return fmt.Errorf("accounts and auth history start: %w", err)
	}
	if first.Valid && first.Time.After(from) {
		from = first.Time.UTC()
	}
	f.WindowStart = from
	rows, err := p.db().QueryContext(ctx, `
WITH g AS (
    SELECT coalesce(client_user, '') AS u, coalesce(client_host, '') AS h, coalesce(reason, '') AS r,
        coalesce(max(account), '') AS acc, count(*) AS n, max(time) AS last
    FROM events WHERE kind = 'client.auth_error' AND time >= $1 AND time <= $2
    GROUP BY ALL
)
SELECT u, h, r, acc, n, last, count(*) OVER (), sum(n) OVER ()::BIGINT FROM g
ORDER BY n DESC, last DESC, u, h, r LIMIT $3`, from, f.EpochAt, api.MaxFailedLoginGroups)
	if err != nil {
		return fmt.Errorf("accounts and auth failed logins: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l api.FailedLoginFact
		if err := rows.Scan(&l.User, &l.Host, &l.Reason, &l.Account, &l.Count, &l.Last, &f.FailedLoginGroups, &f.FailedLoginsCount); err != nil {
			return err
		}
		l.Last = l.Last.UTC()
		f.FailedLogins = append(f.FailedLogins, l)
	}
	return rows.Err()
}

// captureGaps reports whether event capture was not running for part of
// the window, by Replay's rules: no event stored up to the epoch (capture
// never ran), a time nats-lens was not running (collector.gap) that
// overlaps the window, or a lost connection that captures events
// (EventsConnectionGaps).
func (p *Provider) captureGaps(ctx context.Context, f *api.AccountsFacts) error {
	var captured, stopped bool
	if err := p.db().QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM events WHERE time <= $1),
  EXISTS (SELECT 1 FROM events WHERE kind = 'collector.gap' AND time <= $1 AND received_at >= $2)`, f.EpochAt, f.WindowStart).
		Scan(&captured, &stopped); err != nil {
		return fmt.Errorf("accounts and auth capture: %w", err)
	}
	if !captured || stopped {
		f.CaptureGaps = true
		return nil
	}
	rf := &api.ReplayFacts{A: f.WindowStart, B: f.EpochAt}
	if err := p.eventsConnectionGaps(ctx, rf); err != nil {
		return err
	}
	for _, g := range rf.Gaps {
		if !g.Start.After(f.EpochAt) && (g.End.IsZero() || g.End.After(f.WindowStart)) {
			f.CaptureGaps = true
		}
	}
	return nil
}

// certificates reads the listener certificates each server reported in
// the epoch.
func (p *Provider) certificates(ctx context.Context, f *api.AccountsFacts) error {
	rows, err := p.db().QueryContext(ctx, `
SELECT s.server_name, ss.tls_cert_not_after, ss.cluster_tls_cert_not_after, ss.gateway_tls_cert_not_after,
    ss.leafnode_tls_cert_not_after, ss.websocket_tls_cert_not_after, ss.mqtt_tls_cert_not_after
FROM server_stats ss JOIN servers s USING (server_id) WHERE ss.epoch = ? ORDER BY s.server_name`, f.Epoch)
	if err != nil {
		return fmt.Errorf("accounts and auth certificates: %w", err)
	}
	defer rows.Close()
	listeners := []string{api.ListenerClient, api.ListenerCluster, api.ListenerGateway, api.ListenerLeafnode, api.ListenerWebsocket, api.ListenerMQTT}
	for rows.Next() {
		var name string
		ts := make([]sql.NullTime, len(listeners))
		if err := rows.Scan(&name, &ts[0], &ts[1], &ts[2], &ts[3], &ts[4], &ts[5]); err != nil {
			return err
		}
		for i, t := range ts {
			if t.Valid {
				f.Certificates = append(f.Certificates, api.CertificateFact{Server: name, Listener: listeners[i], NotAfter: t.Time.UTC()})
			}
		}
	}
	return rows.Err()
}

// userJWTs reads the epoch's connections that carry a user JWT, by
// account and user: the soonest expiry and the connections that hold it.
func (p *Provider) userJWTs(ctx context.Context, f *api.AccountsFacts) error {
	rows, err := p.db().QueryContext(ctx, `
WITH c AS (
    SELECT coalesce(account, '') AS account, coalesce(authorized_user, '') AS u, jwt_expires
    FROM connections WHERE epoch = ? AND user_jwt
), g AS (
    SELECT account, u, min(jwt_expires) AS soonest FROM c GROUP BY ALL
)
SELECT g.account, g.u, g.soonest, count(*)
FROM g JOIN c ON c.account = g.account AND c.u = g.u AND c.jwt_expires IS NOT DISTINCT FROM g.soonest
GROUP BY ALL ORDER BY 1, 2`, f.Epoch)
	if err != nil {
		return fmt.Errorf("accounts and auth user JWTs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var u api.UserJWTFact
		var soonest sql.NullTime
		if err := rows.Scan(&u.Account, &u.User, &soonest, &u.Connections); err != nil {
			return err
		}
		if soonest.Valid {
			u.Expires = ptr(soonest.Time.UTC())
		}
		f.UserJWTs = append(f.UserJWTs, u)
	}
	return rows.Err()
}
