package store

import (
	"sort"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

var connectionCols = []string{
	"epoch", "server_id", "cid", "kind", "type", "name", "lang", "version", "account", "authorized_user",
	"ip", "port", "start_time", "last_activity", "rtt_ns", "pending_bytes", "in_msgs", "out_msgs",
	"in_bytes", "out_bytes", "stalls", "subscriptions", "tls_version", "name_tag",
}

func (w *writer) writeConnections(v *collect.ServerView) {
	if v.Connz == nil {
		return
	}
	for _, c := range v.Connz.Conns {
		if c.NameTag != "" {
			// CONNZ gives the account's name tag with each connection.
			w.ids.account(c.Account, c.NameTag, false)
		}
		w.insert("connections", connectionCols, w.epoch, v.Info.ID, i64(c.Cid), c.Kind, c.Type, c.Name, c.Lang,
			c.Version, c.Account, c.AuthorizedUser, c.IP, c.Port, ts(c.Start), ts(c.LastActivity), durNS(c.RTT),
			c.Pending, c.InMsgs, c.OutMsgs, c.InBytes, c.OutBytes, c.Stalls, int64(c.NumSubs), c.TLSVersion, c.NameTag)
	}
}

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
				consumers += len(s.Consumer)
			}
			vals = append(vals, i64(d.Memory), i64(d.Store), i64(d.ReservedMemory), i64(d.ReservedStore),
				i64(d.API.Total), i64(d.API.Errors), len(d.Streams), consumers)
		} else {
			vals = append(vals, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		w.insert("account_stats", accountStatsCols, vals...)
	}
}
