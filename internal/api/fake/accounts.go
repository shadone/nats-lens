package fake

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// authSpec is what a scenario's Accounts and auth page reads beyond the
// stream account's use, which comes from the servers and streams: the
// lookups, the other accounts, failed logins, certificates and user JWTs.
type authSpec struct {
	// app is the lookup of the stream account (Account); nil when it was
	// never looked up.
	app *lookupSpec
	// others are the accounts besides Account; they hold no connections
	// and no streams.
	others []accountSpec
	// failed are the clients that fail to log in again and again.
	failed []failedLoginSpec
	// certs are each answering server's listener certificates, by
	// listener, as offsets from FixedNow.
	certs func(sp serverSpec) map[string]time.Duration
	// userJWTs are the expiries of the user JWTs, by user, as offsets
	// from FixedNow; zero means a JWT without expiry. Connections of a
	// user not listed hold no JWT.
	userJWTs map[string]time.Duration
	// users name the user of some connections, by connection key
	// (server/cid); the others authenticate as app.
	users map[string]string
}

// lookupSpec is an account's ACCOUNTZ lookup.
type lookupSpec struct {
	jwt bool
	// expires is the account JWT's expiry as an offset from FixedNow;
	// zero means none.
	expires          time.Duration
	imports, exports int32
	// connLimit, diskLimit and memLimit are the JWT's limits, -1 none;
	// disk and memory both 0 disable JetStream for the account.
	connLimit, diskLimit, memLimit int64
}

// accountSpec is an account besides Account.
type accountSpec struct {
	id, name string
	system   bool
	subs     int64
	lookup   *lookupSpec
}

// failedLoginSpec is a client that fails to log in every interval from
// since (fixed frame) on, burst times in a row spaced by spacing (once
// when burst is 0), on the first of servers that is up at the time.
type failedLoginSpec struct {
	user, host, reason, account string
	servers                     []string
	since                       time.Time
	every                       time.Duration
	burst                       int
	spacing                     time.Duration
}

// upServer is the first of names whose server is up at the scenario time
// at; the first when none is.
func (s scen) upServer(names []string, at time.Time) string {
	for _, n := range names {
		if sp, ok := s.sc.server(n); ok && !s.downAt(sp, at) {
			return n
		}
	}
	return names[0]
}

// lookupAge is how long before an epoch the demo's accounts were looked
// up: in the epoch 3 minutes earlier, 2 seconds after it started, as the
// collector reads a lookup after the epoch's collection.
const lookupAge = 3*time.Minute - 2*time.Second

// authEvents are the failed logins of the scenario up to now, as the
// capture stores them: kind client.auth_error, the user in the payload.
func (s scen) authEvents() []*v1.Event {
	if s.sc.auth == nil {
		return nil
	}
	var out []*v1.Event
	for _, l := range s.sc.auth.failed {
		var times []time.Time
		for start := l.since.Add(s.shift); !start.After(s.dataEnd()); start = start.Add(l.every) {
			for i := range max(l.burst, 1) {
				if at := start.Add(time.Duration(i) * l.spacing); !at.After(s.dataEnd()) {
					times = append(times, at)
				}
			}
		}
		for _, at := range times {
			server := s.upServer(l.servers, at)
			raw, _ := json.Marshal(map[string]any{"client": map[string]string{"user": l.user, "host": l.host, "acc": l.account},
				"reason": l.reason})
			out = append(out, &v1.Event{Time: ts(at), Source: srcNATS, Kind: "client.auth_error", Type: "CLIENT.AUTH_ERROR",
				Server: server, Account: l.account, ClientHost: l.host, Reason: l.reason, Raw: string(raw),
				Summary: fmt.Sprintf("authentication failed on %s for user %s from %s: %s", server, cmp.Or(l.user, "-"), l.host, l.reason)})
		}
	}
	return out
}

// eventUser is the user an authentication error's payload names.
func eventUser(e *v1.Event) string {
	var raw struct {
		Client struct {
			User string `json:"user"`
		} `json:"client"`
	}
	_ = json.Unmarshal([]byte(e.GetRaw()), &raw)
	return raw.Client.User
}

// accounts are the scenario's accounts at the epoch that started at at,
// with their use and lookups, in identifier order.
func (s scen) accounts(at time.Time) []api.AccountFact {
	var conns, leafs int32
	var subs int64
	for _, c := range s.connections(at) {
		conns++
		subs += c.GetSubscriptions()
	}
	for _, sp := range s.sc.servers {
		if !s.downAt(sp, at) {
			leafs += s.leafCount(sp, at)
		}
	}
	var disk, mem int64
	for _, sp := range s.sc.streams {
		b := int64(sp.bytes) * int64(len(s.sc.peers(sp)))
		if s.sc.memoryStorage(sp.name) {
			mem += b
		} else {
			disk += b
		}
	}
	app := api.AccountFact{Account: Account, Connections: ptr(conns), Leafnodes: ptr(leafs), Subscriptions: ptr(subs),
		JSStorage: ptr(disk), JSMemory: ptr(mem), Streams: ptr(int32(len(s.sc.streams)))}
	if a := s.sc.auth; a != nil {
		app.Details = s.details(a.app, at)
	}
	out := []api.AccountFact{app}
	if s.sc.auth != nil {
		for _, o := range s.sc.auth.others {
			out = append(out, api.AccountFact{Account: o.id, Name: o.name, IsSystem: o.system, Connections: ptr(int32(0)),
				Leafnodes: ptr(int32(0)), Subscriptions: ptr(o.subs), JSStorage: ptr(int64(0)), JSMemory: ptr(int64(0)),
				Streams: ptr(int32(0)), Details: s.details(o.lookup, at)})
		}
	}
	slices.SortFunc(out, func(a, b api.AccountFact) int { return strings.Compare(a.Account, b.Account) })
	return out
}

// details is the lookup as read lookupAge before the epoch at at; nil
// without one.
func (s scen) details(l *lookupSpec, at time.Time) *api.AccountDetails {
	if l == nil {
		return nil
	}
	d := &api.AccountDetails{At: at.Add(-lookupAge), HasJWT: l.jwt, Imports: l.imports, Exports: l.exports}
	if l.jwt {
		d.LimitConnections, d.LimitJSDisk, d.LimitJSMemory = ptr(l.connLimit), ptr(l.diskLimit), ptr(l.memLimit)
		if l.expires != 0 {
			exp := FixedNow.Add(l.expires + s.shift)
			d.JWTExpires = &exp
			d.Expired = !exp.After(at)
		}
	}
	return d
}

// AccountsAndAuth builds the facts of the range's newest epoch from the
// scenario: its accounts, the failed logins grouped from its events, the
// answering servers' certificates and the user JWTs of its connections.
func (p *Provider) AccountsAndAuth(_ context.Context, req *v1.GetAccountsAndAuthRequest) (*api.AccountsFacts, error) {
	s := p.scen()
	at, n, ok := s.epochBefore(req.GetRange())
	if !ok {
		return &api.AccountsFacts{}, nil
	}
	f := &api.AccountsFacts{Epoch: n, EpochAt: at, Accounts: s.accounts(at), WindowStart: at.Add(-api.FailedLoginWindow)}
	if start := s.dataStart(); start.After(f.WindowStart) {
		f.WindowStart = start
	}
	all := p.events(s)
	s.failedLogins(f, all)
	captureGaps(f, all)
	f.Certificates = s.certificates(at)
	f.UserJWTs = s.userJWTs(at)
	return f, nil
}

// failedLogins groups the authentication errors of the window by user,
// address and reason, as the store does in SQL.
func (s scen) failedLogins(f *api.AccountsFacts, all []*v1.Event) {
	type key struct{ user, host, reason string }
	groups := map[key]*api.FailedLoginFact{}
	for _, e := range all {
		t := e.GetTime().AsTime()
		if e.GetKind() != "client.auth_error" || t.Before(f.WindowStart) || t.After(f.EpochAt) {
			continue
		}
		k := key{eventUser(e), e.GetClientHost(), e.GetReason()}
		g := groups[k]
		if g == nil {
			g = &api.FailedLoginFact{User: k.user, Host: k.host, Reason: k.reason}
			groups[k] = g
		}
		g.Count++
		g.Account = max(g.Account, e.GetAccount())
		if t.After(g.Last) {
			g.Last = t
		}
		f.FailedLoginsCount++
	}
	for _, g := range groups {
		f.FailedLogins = append(f.FailedLogins, *g)
	}
	f.FailedLoginGroups = int32(len(groups))
}

// captureGaps applies the store's rules to the scenario's events: events
// were captured when any is stored up to the epoch; capture stopped for
// part of the window when a collector.gap overlaps it or a lost events
// connection (api.EventsConnectionGaps) does.
func captureGaps(f *api.AccountsFacts, all []*v1.Event) {
	var conns []*v1.Event
	for i := len(all) - 1; i >= 0; i-- {
		e := all[i]
		if e.GetTime().AsTime().After(f.EpochAt) {
			continue
		}
		f.EventsCaptured = true
		if e.GetKind() == "collector.gap" && !e.GetReceivedAt().AsTime().Before(f.WindowStart) {
			f.CaptureGaps = true
		}
		if e.GetSource() == v1.EventSource_EVENT_SOURCE_COLLECTOR {
			conns = append(conns, e)
		}
	}
	f.CaptureGaps = f.CaptureGaps || slices.ContainsFunc(api.EventsConnectionGaps(conns), func(g api.ReplayGap) bool {
		return !g.Start.After(f.EpochAt) && (g.End.IsZero() || g.End.After(f.WindowStart))
	})
}

// certificates are the listener certificates of every server of the
// scenario at the epoch at at: a server down then is read from its newest
// epoch up, at most api.AccountDetailsMaxAge before, with that epoch's
// time as SeenAt, as the store reads a server whose VARZ did not answer.
func (s scen) certificates(at time.Time) []api.CertificateFact {
	if s.sc.auth == nil || s.sc.auth.certs == nil {
		return nil
	}
	var out []api.CertificateFact
	for _, sp := range s.sc.servers {
		var seen time.Time
		if s.downAt(sp, at) {
			for t := at.Add(-sampleEvery); !t.Before(at.Add(-api.AccountDetailsMaxAge)) && !t.Before(s.dataStart()); t = t.Add(-sampleEvery) {
				if !s.downAt(sp, t) {
					seen = t
					break
				}
			}
			if seen.IsZero() {
				continue
			}
		}
		for listener, in := range s.sc.auth.certs(sp) {
			out = append(out, api.CertificateFact{Server: sp.name, Listener: listener, NotAfter: FixedNow.Add(in + s.shift), SeenAt: seen})
		}
	}
	return out
}

// userOf is the user a connection authenticates as.
func (sc *scenario) userOf(key string) string {
	if sc.auth != nil {
		if u, ok := sc.auth.users[key]; ok {
			return u
		}
	}
	return "app"
}

// userJWTs groups the connections at at that hold a user JWT by user:
// every connection of a user holds a JWT of the same expiry.
func (s scen) userJWTs(at time.Time) []api.UserJWTFact {
	if s.sc.auth == nil || len(s.sc.auth.userJWTs) == 0 {
		return nil
	}
	counts := map[string]int32{}
	for _, c := range s.connections(at) {
		if _, ok := s.sc.auth.userJWTs[c.GetUser()]; ok {
			counts[c.GetUser()]++
		}
	}
	var out []api.UserJWTFact
	for _, u := range slices.Sorted(maps.Keys(counts)) {
		f := api.UserJWTFact{Account: Account, User: u, Connections: counts[u]}
		if in := s.sc.auth.userJWTs[u]; in != 0 {
			f.Expires = ptr(FixedNow.Add(in + s.shift))
		}
		out = append(out, f)
	}
	return out
}

// memoryStorage reports whether a stream is a bucket kept in memory.
func (sc *scenario) memoryStorage(stream string) bool {
	for _, b := range sc.buckets {
		if b.stream == stream {
			return b.storage == "memory"
		}
	}
	return false
}
