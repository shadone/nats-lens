package collect

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// AccountConn is a connection inside one account, over which the
// collector asks the account's micro services. The collector owns no such
// connection: Conn returns the current one, nil while there is none.
type AccountConn struct {
	// Label is the account's name in the configuration.
	Label string
	Conn  func() *nats.Conn
}

// MicroRequest is one of the framework's requests the collector sends.
type MicroRequest string

const (
	// MicroInfo is $SRV.INFO: each instance's version, description and
	// endpoints.
	MicroInfo MicroRequest = "INFO"
	// MicroStats is $SRV.STATS: each instance's counters per endpoint.
	MicroStats MicroRequest = "STATS"
)

// Subject is the request's subject, which every instance of every service
// in the account answers.
func (r MicroRequest) Subject() string { return micro.APIPrefix + "." + string(r) }

// MicroState is how a request to an account went.
type MicroState string

const (
	// MicroOK means at least one instance replied.
	MicroOK MicroState = "ok"
	// MicroNoResponders means the server answered that nothing in the
	// account listens: the account has no micro service.
	MicroNoResponders MicroState = "no_responders"
	// MicroTimeout means no reply came within the collect window.
	MicroTimeout MicroState = "timeout"
	// MicroError means the request could not be made or answered: no
	// connection, a permissions violation, or only undecodable replies.
	MicroError MicroState = "error"
)

// MicroAsked records one request to one account.
type MicroAsked struct {
	Request MicroRequest
	State   MicroState
	// Replies are the replies that decoded.
	Replies int
	// Error is the reason of an error state, or what did not decode.
	Error string
}

// MicroAccount is what one account's micro services answered in an epoch.
type MicroAccount struct {
	// Label is the account's name in the configuration.
	Label string
	// Account is the account as the servers name it (its public key in
	// operator mode), or Label when the server was not asked or refused.
	Account string
	// Asked are the requests sent: STATS every epoch, INFO when due.
	Asked []MicroAsked
	// Infos and Stats are the decoded replies, one per instance, sorted
	// by service name and instance ID.
	Infos []micro.Info
	Stats []micro.Stats
}

// instanceKey names one service instance within an account.
type instanceKey struct{ service, id string }

// microAccount is what the collector remembers of one account between
// epochs.
type microAccount struct {
	// infoAsked is the start of the epoch that last sent INFO; zero
	// before the first.
	infoAsked time.Time
	// endpoints are the endpoints of each described instance, from its
	// newest INFO reply.
	endpoints map[instanceKey][]micro.EndpointInfo
	// resolvedOn is the connection account was asked on; account is the
	// servers' name for the account, empty until known.
	resolvedOn *nats.Conn
	account    string
	// lookupDeniedAt is the start of the epoch whose account lookup the
	// server last refused; zero when it did not.
	lookupDeniedAt time.Time
	// watch takes the refusals of the collector's requests on watchedOn.
	watchedOn *nats.Conn
	watch     *microWatch
	// key is the account this label's answers were last kept under, empty
	// before it asked over a connection.
	key string
	// statsFailed is set when this label's last STATS failed (refused,
	// not connected): it does not ask for another label.
	statsFailed bool
	// sameAs is the earlier label that asks this label's account for it,
	// empty when this label asks.
	sameAs string
	// refused holds each request's refusal as last logged.
	refused map[MicroRequest]string
}

// SetAccountConns sets the account connections asked from the next epoch
// on; nil asks none.
func (c *Collector) SetAccountConns(conns []AccountConn) {
	c.accountConns.Store(&conns)
}

// askMicro asks every account connection's micro services in parallel,
// within one window from now, and returns the answers sorted by label.
func (c *Collector) askMicro(ctx context.Context, started time.Time) []MicroAccount {
	var conns []AccountConn
	if p := c.accountConns.Load(); p != nil {
		conns = *p
	}
	if len(conns) == 0 {
		clear(c.microAccounts)
		clear(c.microKnown)
		return nil
	}
	// Forget the labels no longer configured.
	for label := range c.microAccounts {
		if !slices.ContainsFunc(conns, func(ac AccountConn) bool { return ac.Label == label }) {
			delete(c.microAccounts, label)
		}
	}
	deadline := time.Now().Add(c.window)
	out := make([]MicroAccount, len(conns))
	states := make([]*microAccount, len(conns))
	// An account already asked under an earlier label over a connected
	// connection, whose last STATS was not refused, is not asked again
	// under a later one. A label's account is the one its answers were
	// last kept under: the servers' name for it, or the label when the
	// lookup was refused. The choice is made before the requests: a
	// connection that drops during them leaves its account without
	// answers in that epoch.
	asking := map[string]string{}
	var wg sync.WaitGroup
	for i, ac := range conns {
		st := c.microAccounts[ac.Label]
		if st == nil {
			st = &microAccount{endpoints: map[instanceKey][]micro.EndpointInfo{}, refused: map[MicroRequest]string{}}
			c.microAccounts[ac.Label] = st
		}
		states[i] = st
		if _, ok := asking[st.key]; ok && st.key != "" {
			continue
		}
		if nc := connOf(ac); st.key != "" && nc != nil && nc.IsConnected() && !st.statsFailed {
			asking[st.key] = ac.Label
		}
		known := knownSet(c.microKnown[st.key])
		wg.Go(func() { out[i] = c.askAccount(ctx, ac, st, known, started, deadline) })
	}
	wg.Wait()
	// One MicroAccount per account and epoch: of the labels that name one
	// account, the first whose STATS was asked and not refused is kept,
	// and a label that could not ask, or was refused, only when no other
	// label of its account did better.
	var kept []MicroAccount
	var keptState []*microAccount
	at := map[string]int{}
	for i, m := range out {
		st := states[i]
		if m.Label == "" {
			c.noteSameAccount(conns[i].Label, st, asking[st.key])
			continue
		}
		if connected(m) {
			st.key = m.Account
		}
		st.statsFailed = !statsAnswered(m)
		j, ok := at[m.Account]
		switch {
		case !ok:
			at[m.Account] = len(kept)
			kept, keptState = append(kept, m), append(keptState, st)
			c.noteSameAccount(m.Label, st, "")
		case statsAnswered(kept[j]) || !statsAnswered(m):
			c.noteSameAccount(m.Label, st, kept[j].Label)
		default:
			c.noteSameAccount(kept[j].Label, keptState[j], m.Label)
			kept[j], keptState[j] = m, st
			c.noteSameAccount(m.Label, st, "")
		}
	}
	// The instances that answered STATS are known per account, whichever
	// label asked.
	for _, m := range kept {
		for _, a := range m.Asked {
			if a.Request == MicroStats && (a.State == MicroOK || a.State == MicroNoResponders) {
				c.microKnown[m.Account] = nextKnown(c.microKnown[m.Account], m.Stats)
			}
		}
	}
	for key := range c.microKnown {
		if _, ok := at[key]; !ok && !slices.ContainsFunc(states, func(st *microAccount) bool { return st.key == key }) {
			delete(c.microKnown, key)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Label < kept[j].Label })
	return kept
}

// statsAnswered reports whether the account's STATS went out and was
// neither refused nor failed: ok, no responders or timeout.
func statsAnswered(m MicroAccount) bool {
	for _, a := range m.Asked {
		if a.Request == MicroStats {
			return a.State != MicroError
		}
	}
	return false
}

// connected reports whether the account's requests went out over a
// connection.
func connected(m MicroAccount) bool {
	for _, a := range m.Asked {
		if a.Error != notConnected {
			return true
		}
	}
	return false
}

// notConnected is the error of a request without a connection.
const notConnected = "account connection not connected"

// connOf returns the account connection's current connection, or nil.
func connOf(ac AccountConn) *nats.Conn {
	if ac.Conn == nil {
		return nil
	}
	return ac.Conn()
}

// noteSameAccount logs once that label names the account first asks for
// it, and once when label asks for itself again; first is empty when
// label is asked itself.
func (c *Collector) noteSameAccount(label string, st *microAccount, first string) {
	if st.sameAs == first {
		return
	}
	if first != "" {
		c.log.Info("accounts entry names an account asked under another entry; its micro services are not asked under this one",
			"label", label, "asked_under", first, "account", st.key)
	} else {
		c.log.Info("accounts entry asks its micro services itself again", "label", label, "account", st.key)
	}
	st.sameAs = first
}

// infoDue reports whether an epoch started at now sends INFO by the
// interval: never sent, the clock stepped back, or the subscription
// interval passed. With the interval 0 only the first epoch is due by
// time.
func (c *Collector) infoDue(st *microAccount, now time.Time) bool {
	if st.infoAsked.IsZero() || now.Before(st.infoAsked) {
		return true
	}
	return c.subsInterval > 0 && now.Sub(st.infoAsked) >= c.subsInterval-subscriptionsSlack
}

// lookupRetry is how long after a refused account lookup it is made again.
const lookupRetry = 10 * time.Minute

// userInfoToken is the exchange token of the account lookup.
const userInfoToken = "USER"

// askAccount sends STATS, and INFO when due, to one account, and INFO
// again when STATS names an instance no INFO described.
// known are the instances that answered the account's last answered
// STATS, under any label; the requests wait for them.
func (c *Collector) askAccount(ctx context.Context, ac AccountConn, st *microAccount, known map[instanceKey]bool,
	started, deadline time.Time) MicroAccount {
	res := MicroAccount{Label: ac.Label, Account: ac.Label}
	wantInfo := c.infoDue(st, started)
	nc := connOf(ac)
	if nc == nil || !nc.IsConnected() {
		if st.account != "" {
			res.Account = st.account
		}
		for _, r := range asked(wantInfo) {
			res.Asked = append(res.Asked, MicroAsked{Request: r, State: MicroError, Error: notConnected})
		}
		return res
	}
	if st.watchedOn != nc {
		st.watch = newMicroWatch()
		st.watch.install(nc)
		st.watchedOn = nc
	}
	reqs := map[string]string{string(MicroStats): MicroStats.Subject()}
	if wantInfo {
		reqs[string(MicroInfo)] = MicroInfo.Subject()
		st.infoAsked = started
	}
	lookUp := st.resolvedOn != nc && (st.lookupDeniedAt.IsZero() || started.Before(st.lookupDeniedAt) ||
		started.Sub(st.lookupDeniedAt) >= lookupRetry)
	if lookUp {
		reqs[userInfoToken] = userInfoSubject
	}
	got := exchange(ctx, nc, st.watch, reqs, deadline, microQuiet, known, string(MicroStats), string(MicroInfo))
	if lookUp {
		c.resolveMicroAccount(ac.Label, st, nc, got[userInfoToken], started)
	}
	statsAsked, stats := decodeStats(got[string(MicroStats)])
	res.Stats = stats
	var infos []micro.Info
	if wantInfo {
		var a MicroAsked
		a, infos = decodeInfos(got[string(MicroInfo)])
		res.Asked = append(res.Asked, a)
	}
	pruneDescribed(st, statsAsked, stats)
	describe(st, infos)
	if !wantInfo && undescribed(st, stats) {
		// A new instance: ask INFO in the time left.
		st.infoAsked = started
		var a MicroAsked
		a, infos = decodeInfos(exchange(ctx, nc, st.watch, map[string]string{string(MicroInfo): MicroInfo.Subject()},
			deadline, microQuiet, instancesOf(stats), string(MicroInfo))[string(MicroInfo)])
		res.Asked = append(res.Asked, a)
		describe(st, infos)
	}
	res.Asked = append(res.Asked, statsAsked)
	res.Infos = infos
	fillEndpointSubjects(st, res.Stats)
	if st.account != "" {
		res.Account = st.account
	}
	sort.Slice(res.Asked, func(i, j int) bool { return res.Asked[i].Request < res.Asked[j].Request })
	c.logRefusal(ac.Label, st, res.Asked)
	return res
}

// logRefusal logs the account's refused requests once, and again only
// when a request's refusal changes or every refusal has ended, rather
// than every epoch. A request not sent this epoch keeps its last state.
func (c *Collector) logRefusal(label string, st *microAccount, asked []MicroAsked) {
	changed, had := false, len(st.refused) > 0
	for _, a := range asked {
		text := ""
		if strings.Contains(strings.ToLower(a.Error), "permissions violation") {
			text = a.Error
		}
		if st.refused[a.Request] == text {
			continue
		}
		if text == "" {
			delete(st.refused, a.Request)
			continue
		}
		st.refused[a.Request] = text
		changed = true
	}
	switch {
	case changed:
		var all []string
		for _, r := range []MicroRequest{MicroInfo, MicroStats} {
			if text := st.refused[r]; text != "" {
				all = append(all, string(r)+": "+text)
			}
		}
		c.log.Warn("micro service requests refused; the account user needs publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>",
			"account", label, "err", strings.Join(all, "; "))
	case had && len(st.refused) == 0:
		c.log.Info("micro service requests permitted again", "account", label)
	}
}

// microKnownMisses is how many answered STATS in a row an instance may
// miss and still be waited for: one late reply cut by the cap is not
// forgotten at once, and a stopped instance holds up three epochs.
const microKnownMisses = 3

// nextKnown returns the known instances after an answered STATS: those
// that answered with no misses, the others with one more, those that
// missed microKnownMisses dropped.
func nextKnown(prev map[instanceKey]int, stats []micro.Stats) map[instanceKey]int {
	out := map[instanceKey]int{}
	for k, n := range prev {
		if n+1 < microKnownMisses {
			out[k] = n + 1
		}
	}
	for _, s := range stats {
		out[instanceKey{s.Name, s.ID}] = 0
	}
	return out
}

// knownSet returns the instances of a known map.
func knownSet(known map[instanceKey]int) map[instanceKey]bool {
	out := make(map[instanceKey]bool, len(known))
	for k := range known {
		out[k] = true
	}
	return out
}

// instancesOf returns the instances of STATS replies.
func instancesOf(stats []micro.Stats) map[instanceKey]bool {
	out := map[instanceKey]bool{}
	for _, s := range stats {
		out[instanceKey{s.Name, s.ID}] = true
	}
	return out
}

// asked lists the requests of an epoch: STATS, and INFO when due.
func asked(info bool) []MicroRequest {
	if info {
		return []MicroRequest{MicroInfo, MicroStats}
	}
	return []MicroRequest{MicroStats}
}

// pruneDescribed forgets the instances that answered neither this STATS
// nor INFO, once STATS was answered or the account has no services, so
// that stopped instances do not pile up. A failed STATS forgets nothing.
func pruneDescribed(st *microAccount, a MicroAsked, stats []micro.Stats) {
	if a.State != MicroOK && a.State != MicroNoResponders {
		return
	}
	alive := map[instanceKey]bool{}
	for _, s := range stats {
		alive[instanceKey{s.Name, s.ID}] = true
	}
	for k := range st.endpoints {
		if !alive[k] {
			delete(st.endpoints, k)
		}
	}
}

// describe remembers the endpoints of every instance INFO described.
func describe(st *microAccount, infos []micro.Info) {
	for _, in := range infos {
		eps := in.Endpoints
		if eps == nil {
			eps = []micro.EndpointInfo{}
		}
		st.endpoints[instanceKey{in.Name, in.ID}] = eps
	}
}

// undescribed reports whether an instance in stats has no INFO yet.
func undescribed(st *microAccount, stats []micro.Stats) bool {
	for _, s := range stats {
		if _, ok := st.endpoints[instanceKey{s.Name, s.ID}]; !ok {
			return true
		}
	}
	return false
}

// fillEndpointSubjects sets the subject and queue group of an endpoint
// that STATS did not report from the instance's newest INFO.
func fillEndpointSubjects(st *microAccount, stats []micro.Stats) {
	for _, s := range stats {
		eps := st.endpoints[instanceKey{s.Name, s.ID}]
		for _, e := range s.Endpoints {
			if e == nil || (e.Subject != "" && e.QueueGroup != "") {
				continue
			}
			for _, info := range eps {
				if info.Name != e.Name {
					continue
				}
				if e.Subject == "" {
					e.Subject = info.Subject
				}
				if e.QueueGroup == "" {
					e.QueueGroup = info.QueueGroup
				}
				break
			}
		}
	}
}
