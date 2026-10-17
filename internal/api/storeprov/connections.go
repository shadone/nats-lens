package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListConnections reads the connections of the range's newest epoch,
// ordered by server name and connection ID, a page at a time. Later pages
// read the epoch of the first, so that a new epoch does not shift them.
func (p *Provider) ListConnections(ctx context.Context, req *v1.ListConnectionsRequest) (*v1.ListConnectionsResponse, error) {
	cur, err := api.DecodeCursor(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	if cur.Epoch != 0 {
		var n int
		if err := p.db().QueryRowContext(ctx, `SELECT count(*) FROM epochs WHERE epoch = ?`, cur.Epoch).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, fmt.Errorf("%w: epoch %d of the page token is no longer stored; read the first page again",
				api.ErrFailedPrecondition, cur.Epoch)
		}
		e, ok = epochInfo{epoch: cur.Epoch}, true
	}
	if !ok {
		return &v1.ListConnectionsResponse{}, nil
	}
	res := &v1.ListConnectionsResponse{Epoch: e.epoch}
	// The epoch's stored decision, as the client checks took it; an epoch
	// without one counts as on.
	stored, err := checks.StoredLoopback(ctx, p.db(), e.epoch)
	if err != nil {
		return nil, err
	}
	loopback := !p.LoopbackNotTooling && stored
	// tooling is the same rule the client checks use.
	tooling := `(list_contains(?::VARCHAR[], coalesce(c.name, '')) OR (? AND coalesce(c.ip, '') IN ('127.0.0.1', '::1')))`
	names := append([]string{}, p.ToolingNames...)
	const base = `
FROM connections c JOIN servers s USING (server_id)
WHERE c.epoch = ? AND (? = '' OR s.server_name = ?) AND (? = '' OR c.account = ?)
  AND contains(coalesce(c.name, ''), ?)`
	baseArgs := []any{e.epoch, req.GetServer(), req.GetServer(), req.GetAccount(), req.GetAccount(), req.GetNameContains()}
	if err := p.db().QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE `+tooling+`)`+base,
		append([]any{names, loopback}, baseArgs...)...).Scan(&res.ToolingHidden); err != nil {
		return nil, err
	}
	from, args := base, baseArgs
	if req.GetIncludeTooling() {
		res.ToolingHidden = 0
	} else {
		from, args = base+` AND NOT `+tooling, append(append([]any{}, baseArgs...), names, loopback)
	}
	if err := p.db().QueryRowContext(ctx, `SELECT count(*)`+from, args...).Scan(&res.TotalCount); err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
SELECT s.server_name, c.cid, c.name, c.lang, c.version, c.account, c.authorized_user, c.ip, c.port, c.start_time,
  c.last_activity, c.rtt_ns, c.pending_bytes, c.in_msgs, c.out_msgs, c.in_bytes, c.out_bytes, c.stalls, c.subscriptions`+
		from+` ORDER BY s.server_name, c.cid LIMIT ? OFFSET ?`, append(args, req.GetPageSize(), cur.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c v1.Connection
		var name, lang, version, account, user, ip sql.NullString
		var started, active sql.NullTime
		var rtt sql.NullInt64
		// Numbers scan into their optional fields: NULL leaves them unset.
		if err := rows.Scan(&c.Server, &c.Cid, &name, &lang, &version, &account, &user, &ip, &c.Port, &started, &active,
			&rtt, &c.PendingBytes, &c.InMsgs, &c.OutMsgs, &c.InBytes, &c.OutBytes, &c.Stalls, &c.Subscriptions); err != nil {
			return nil, err
		}
		c.Name, c.Lang, c.Version, c.Account, c.User, c.Ip = name.String, lang.String, version.String, account.String, user.String, ip.String
		c.Tooling = slices.Contains(p.ToolingNames, c.GetName()) || (loopback && (c.GetIp() == "127.0.0.1" || c.GetIp() == "::1"))
		c.StartTime, c.LastActivity = ts(ntime(started)), ts(ntime(active))
		if rtt.Valid {
			c.Rtt = dur(time.Duration(rtt.Int64))
		}
		c.Key = fmt.Sprintf("%s/%d", c.GetServer(), c.GetCid())
		res.Connections = append(res.Connections, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if next := cur.Offset + len(res.GetConnections()); next < int(res.GetTotalCount()) {
		res.NextPageToken = api.EncodeCursor(api.Cursor{Offset: next, Epoch: e.epoch})
	}
	return res, nil
}
