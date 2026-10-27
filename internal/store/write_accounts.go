package store

import (
	"sort"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

var accountStatsCols = []string{
	"epoch", "server_id", "account", "conns", "leafnodes", "total_conns", "subscriptions",
	"sent_msgs", "sent_bytes", "received_msgs", "received_bytes", "slow_consumers",
	"js_memory", "js_storage", "js_reserved_memory", "js_reserved_storage", "js_api_total", "js_api_errors",
	"js_streams", "js_consumers",
}

// accountRow merges one account's STATZ numbers and JSZ usage on one server.
type accountRow struct {
	stat   *server.AccountStat
	detail *server.AccountDetail
}

func (w *writer) writeAccountStats(v *collect.ServerView) {
	system := ""
	if v.Accountz != nil {
		system = v.Accountz.SystemAccount
		for _, a := range v.Accountz.Accounts {
			w.ids.account(a, "", a == system)
		}
	}
	if system == "" && v.Varz != nil {
		system = v.Varz.SystemAccount
	}

	rows := map[string]*accountRow{}
	get := func(acc string) *accountRow {
		r, ok := rows[acc]
		if !ok {
			r = &accountRow{}
			rows[acc] = r
		}
		return r
	}
	if v.AccountStatz != nil {
		for _, a := range v.AccountStatz.Accounts {
			if a == nil {
				continue
			}
			get(a.Account).stat = a
			w.ids.account(a.Account, a.Name, a.Account == system)
		}
	}
	if v.Jsz != nil {
		for _, d := range v.Jsz.AccountDetails {
			if d == nil {
				continue
			}
			get(d.Id).detail = d
			w.ids.account(d.Id, d.Name, d.Id == system)
		}
	}

	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, acc := range names {
		r := rows[acc]
		vals := []any{w.epoch, v.Info.ID, acc}
		if s := r.stat; s != nil {
			vals = append(vals, s.Conns, s.LeafNodes, s.TotalConns, int64(s.NumSubs),
				s.Sent.Msgs, s.Sent.Bytes, s.Received.Msgs, s.Received.Bytes, s.SlowConsumers)
		} else {
			vals = append(vals, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		if d := r.detail; d != nil {
			consumers := 0
			for _, s := range d.Streams {
				for _, c := range s.Consumer {
					if c != nil {
						consumers++
					}
				}
			}
			vals = append(vals, i64(d.Memory), i64(d.Store), i64(d.ReservedMemory), i64(d.ReservedStore),
				i64(d.API.Total), i64(d.API.Errors), len(d.Streams), consumers)
		} else {
			vals = append(vals, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		w.insert("account_stats", accountStatsCols, vals...)
	}
}

var accountDetailCols = []string{
	"epoch", "server_id", "account", "looked_up_at", "account_name", "is_system", "expired", "imports", "exports",
	"has_jwt", "jwt_issued_at", "jwt_expires", "limit_connections", "limit_leafnodes", "limit_subscriptions",
	"limit_data", "limit_payload", "limit_js_disk", "limit_js_memory", "limit_js_streams", "limit_js_consumers",
}

// writeAccountDetails writes the epoch's ACCOUNTZ lookups, one row each.
func (w *writer) writeAccountDetails() {
	for _, d := range w.snap.AccountDetails {
		var name any
		if d.Name != "" {
			name = d.Name
		}
		vals := []any{w.epoch, d.ServerID, d.Account, d.LookedUpAt.UTC(), name, d.IsSystem, d.Expired,
			d.Imports, d.Exports, d.HasJWT}
		if d.HasJWT {
			l := d.Limits
			vals = append(vals, ts(d.JWTIssuedAt), ts(d.JWTExpires), l.Connections, l.LeafNodes, l.Subscriptions,
				l.Data, l.Payload, l.JSDisk, l.JSMemory, l.JSStreams, l.JSConsumers)
		} else {
			vals = append(vals, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		w.insert("account_details", accountDetailCols, vals...)
		w.ids.account(d.Account, d.Name, d.IsSystem)
	}
}
