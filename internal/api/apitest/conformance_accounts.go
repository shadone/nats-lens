package apitest

import (
	"fmt"
	"slices"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// accountsAndAuth reads the Accounts and auth page of the system's last
// day.
func (q Q) accountsAndAuth() *v1.GetAccountsAndAuthResponse {
	q.t.Helper()
	return must(q.env.Client.GetAccountsAndAuth(ctx, connect.NewRequest(&v1.GetAccountsAndAuthRequest{System: q.System, Range: q.day()}))).of(q.t)
}

// accountRow finds an account of the system's Accounts and auth page that
// ok accepts.
func accountRow(ok func(*v1.GetAccountsAndAuthResponse, *v1.AccountRow) bool) func(Q) string {
	return func(q Q) string {
		res := q.accountsAndAuth()
		for _, a := range res.GetAccounts() {
			if ok(res, a) {
				return a.GetAccount()
			}
		}
		return ""
	}
}

// expiringRow finds an expiring row of the system's Accounts and auth page
// that ok accepts.
func expiringRow(ok func(*v1.GetAccountsAndAuthResponse, *v1.Expiring) bool) func(Q) string {
	return func(q Q) string {
		res := q.accountsAndAuth()
		for _, e := range res.GetExpiring() {
			if ok(res, e) {
				return fmt.Sprintf("%s %s%s%s %s", e.GetKind(), e.GetServer(), e.GetAccount(), e.GetUser(), e.GetExpires().AsTime())
			}
		}
		return ""
	}
}

// accountsRows are the Accounts and auth states both providers must show.
func accountsRows() []State {
	return []State{
		{"Accounts in operator mode with an account JWT expiring soon", expiringRow(func(res *v1.GetAccountsAndAuthResponse, e *v1.Expiring) bool {
			return res.GetOperatorMode() && e.GetKind() == v1.ExpiringKind_EXPIRING_KIND_ACCOUNT_JWT && e.GetExpires() != nil &&
				e.GetAccount() != "" && e.GetExpires().AsTime().Sub(res.GetEpochAt().AsTime()) > 0
		})},
		{"Accounts in operator mode with an account JWT that never expires, counted in never_expire", accountRow(func(res *v1.GetAccountsAndAuthResponse, a *v1.AccountRow) bool {
			return res.GetOperatorMode() && a.GetDetailsKnown() && a.GetHasJwt() && a.GetJwtExpires() == nil && !a.GetExpired() &&
				a.ConnLimit != nil && a.DiskLimit != nil && a.GetJetstreamEnabled() && res.GetNeverExpire() > 0
		})},
		{"Accounts in config mode: certificates only, no JWT and no limits", func(q Q) string {
			res := q.accountsAndAuth()
			if res.GetEpoch() == 0 || res.GetOperatorMode() || len(res.GetAccounts()) == 0 || len(res.GetExpiring())+int(res.GetExpireLater()) == 0 ||
				res.GetNeverExpire() != 0 {
				return ""
			}
			for _, e := range res.GetExpiring() {
				if e.GetKind() != v1.ExpiringKind_EXPIRING_KIND_SERVER_CERTIFICATE || e.GetServer() == "" || e.GetListener() == "" {
					return ""
				}
			}
			for _, a := range res.GetAccounts() {
				if a.GetHasJwt() || a.ConnLimit != nil || a.DiskLimit != nil || a.JetstreamEnabled != nil || a.GetJwtExpires() != nil {
					return ""
				}
			}
			return fmt.Sprintf("%d accounts, %d certificates listed, %d later", len(res.GetAccounts()), len(res.GetExpiring()), res.GetExpireLater())
		}},
		{"a server certificate expiring within 30 days", expiringRow(func(res *v1.GetAccountsAndAuthResponse, e *v1.Expiring) bool {
			return e.GetKind() == v1.ExpiringKind_EXPIRING_KIND_SERVER_CERTIFICATE && !e.GetExpired() && e.GetListener() == "client" &&
				e.GetExpires().AsTime().After(res.GetEpochAt().AsTime())
		})},
		{"a user JWT expiring soon with the connections that hold it", expiringRow(func(_ *v1.GetAccountsAndAuthResponse, e *v1.Expiring) bool {
			return e.GetKind() == v1.ExpiringKind_EXPIRING_KIND_USER_JWT && e.GetUser() != "" && e.GetAccount() != "" && e.GetConnections() > 1
		})},
		{"failed logins grouped by user, address and reason with the count and the last time", func(q Q) string {
			res := q.accountsAndAuth()
			from, to := res.GetFailedLoginsWindowStart().AsTime(), res.GetEpochAt().AsTime()
			var sum int64
			for _, l := range res.GetFailedLogins() {
				sum += l.GetCount()
			}
			for _, l := range res.GetFailedLogins() {
				if l.GetUser() != "" && l.GetHost() != "" && l.GetReason() != "" && l.GetCount() > 1 && l.GetLast().AsTime().After(from) &&
					!l.GetLast().AsTime().After(to) && res.GetFailedLoginsTotal() >= sum && res.GetFailedLoginGroupsTotal() >= int32(len(res.GetFailedLogins())) {
					return fmt.Sprintf("%s from %s: %d", l.GetUser(), l.GetHost(), l.GetCount())
				}
			}
			return ""
		}},
		{"failed logins of a client that gave no user", func(q Q) string {
			for _, l := range q.accountsAndAuth().GetFailedLogins() {
				if l.GetUser() == "" && l.GetHost() != "" && l.GetCount() > 0 {
					return l.GetHost()
				}
			}
			return ""
		}},
		{"an account not looked up yet: no imports, exports, limits or JWT", accountRow(func(_ *v1.GetAccountsAndAuthResponse, a *v1.AccountRow) bool {
			return !a.GetDetailsKnown() && a.Imports == nil && a.Exports == nil && a.ConnLimit == nil && a.DiskLimit == nil &&
				a.GetJwtExpires() == nil && a.GetDetailsAt() == nil && !a.GetHasJwt()
		})},
		{"an account whose JWT disables JetStream: disk limit 0 and jetstream_enabled false", accountRow(func(_ *v1.GetAccountsAndAuthResponse, a *v1.AccountRow) bool {
			return a.GetDetailsKnown() && a.GetHasJwt() && a.DiskLimit != nil && a.GetDiskLimit() == 0 && a.JetstreamEnabled != nil &&
				!a.GetJetstreamEnabled()
		})},
		{"an account with leaf nodes: those of the server that holds the most counted against the limit, with that server", accountRow(func(_ *v1.GetAccountsAndAuthResponse, a *v1.AccountRow) bool {
			return a.GetLeafnodes() > 0 && a.LimitLeafnodes != nil && a.GetLimitLeafnodes() > 0 && a.GetLimitLeafnodes() <= a.GetLeafnodes() &&
				a.GetLimitLeafnodesServer() != ""
		})},
		{"an account without leaf nodes counts none against the limit and names no server", accountRow(func(_ *v1.GetAccountsAndAuthResponse, a *v1.AccountRow) bool {
			return a.Connections != nil && a.Leafnodes != nil && a.GetLeafnodes() == 0 && a.LimitLeafnodes != nil && a.GetLimitLeafnodes() == 0 &&
				a.GetLimitLeafnodesServer() == ""
		})},
		{"an idle account: no connections, leaf nodes or streams", accountRow(func(_ *v1.GetAccountsAndAuthResponse, a *v1.AccountRow) bool {
			return !a.GetIsSystem() && a.Connections != nil && a.GetConnections() == 0 && a.Leafnodes != nil && a.GetLeafnodes() == 0 &&
				a.Streams != nil && a.GetStreams() == 0
		})},
		{"the Accounts and auth accounts are ListAccounts' accounts, with their names and connections, in every system", everySystem(func(q Q) string {
			res := q.accountsAndAuth()
			ls := must(q.env.Client.ListAccounts(ctx, connect.NewRequest(&v1.ListAccountsRequest{System: q.System, Range: q.day()}))).of(q.t)
			if res.GetEpoch() == 0 {
				return ""
			}
			row := func(id, name string, system bool, conns *int32) string {
				c := "unset"
				if conns != nil {
					c = fmt.Sprint(*conns)
				}
				return fmt.Sprintf("%s %q %v %s", id, name, system, c)
			}
			var got, want []string
			for _, a := range res.GetAccounts() {
				got = append(got, row(a.GetAccount(), a.GetAccountName(), a.GetIsSystem(), a.Connections))
			}
			for _, a := range ls.GetAccounts() {
				want = append(want, row(a.GetAccount(), a.GetAccountName(), a.GetIsSystem(), a.Connections))
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				q.t.Errorf("system %q: accounts %v; ListAccounts %v", q.System, got, want)
				return ""
			}
			if res.GetAccountsTotal() != int32(len(want)) {
				q.t.Errorf("system %q: accounts_total %d, want %d", q.System, res.GetAccountsTotal(), len(want))
				return ""
			}
			return fmt.Sprintf("%d accounts", len(got))
		})},
	}
}
