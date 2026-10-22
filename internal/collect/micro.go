package collect

import (
	"context"
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
	// lookupDenied is set once the server refused the account lookup.
	lookupDenied bool
	// watch takes the refusals of the collector's requests on watchedOn.
	watchedOn *nats.Conn
	watch     *microWatch
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
	p := c.accountConns.Load()
	if p == nil || len(*p) == 0 {
		return nil
	}
	conns := *p
	deadline := time.Now().Add(c.window)
	out := make([]MicroAccount, len(conns))
	var wg sync.WaitGroup
	for i, ac := range conns {
		st := c.microAccounts[ac.Label]
		if st == nil {
			st = &microAccount{endpoints: map[instanceKey][]micro.EndpointInfo{}, refused: map[MicroRequest]string{}}
			c.microAccounts[ac.Label] = st
		}
		wg.Go(func() { out[i] = c.askAccount(ctx, ac, st, started, deadline) })
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
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

// userInfoToken is the exchange token of the account lookup.
const userInfoToken = "USER"

// askAccount sends STATS, and INFO when due, to one account, and INFO
// again when STATS names an instance no INFO described.
func (c *Collector) askAccount(ctx context.Context, ac AccountConn, st *microAccount, started, deadline time.Time) MicroAccount {
	res := MicroAccount{Label: ac.Label, Account: ac.Label}
	wantInfo := c.infoDue(st, started)
	var nc *nats.Conn
	if ac.Conn != nil {
		nc = ac.Conn()
	}
	if nc == nil || !nc.IsConnected() {
		if st.account != "" {
			res.Account = st.account
		}
		for _, r := range asked(wantInfo) {
			res.Asked = append(res.Asked, MicroAsked{Request: r, State: MicroError, Error: "account connection not connected"})
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
	lookUp := st.resolvedOn != nc && !st.lookupDenied
	if lookUp {
		reqs[userInfoToken] = userInfoSubject
	}
	got := exchange(ctx, nc, st.watch, reqs, deadline, microQuiet)
	if lookUp {
		c.resolveMicroAccount(ac.Label, st, nc, got[userInfoToken])
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
			deadline, microQuiet)[string(MicroInfo)])
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
