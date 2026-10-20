// Package collect takes one snapshot (an epoch) of a NATS system through its
// system account: it discovers the servers and gathers every monitoring
// endpoint from each of them, keeping each server's own view.
package collect

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

// Collector takes snapshots over one system account connection.
type Collector struct {
	// nc is the system account connection; SetConn replaces it when the
	// connection is dialled again.
	nc         atomic.Pointer[nats.Conn]
	window     time.Duration
	connzLimit int
	tracker    *Tracker
	log        *slog.Logger
	// raftAccounts are the JetStream accounts seen in the previous epoch.
	raftAccounts []string
	// accountNames are the readable names ACCOUNTZ lookups found, and
	// accountAsked when each account was last looked up.
	accountNames map[string]string
	accountAsked map[string]time.Time
	now          func() time.Time
}

// Options configure a Collector.
type Options struct {
	// Window is how long each request waits for replies.
	Window time.Duration
	// ConnzLimit is the most connections asked of one server.
	ConnzLimit int
	// Tracker remembers servers between epochs. Required.
	Tracker *Tracker
	Log     *slog.Logger
}

// New returns a Collector.
func New(nc *nats.Conn, o Options) *Collector {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	c := &Collector{
		window:       o.Window,
		connzLimit:   o.ConnzLimit,
		tracker:      o.Tracker,
		log:          log,
		accountNames: map[string]string{},
		accountAsked: map[string]time.Time{},
		now:          time.Now,
	}
	c.nc.Store(nc)
	return c
}

// SetConn makes the collector use nc from the next request on.
func (c *Collector) SetConn(nc *nats.Conn) { c.nc.Store(nc) }

func (c *Collector) conn() *nats.Conn { return c.nc.Load() }

// Collect takes one snapshot. It always returns a snapshot; failures are
// recorded on it and make it partial.
func (c *Collector) Collect(ctx context.Context) *Snapshot {
	snap := NewSnapshot(c.now().UTC())
	known := c.tracker.Known(snap.StartedAt)

	// Discovery: stop early once every known server has answered.
	c.request(ctx, snap, Statsz, allNames(known))
	if len(snap.Servers) == 0 {
		snap.Errors = append(snap.Errors, "no server answered "+Statsz.Subject())
	}

	// Detail endpoints run in parallel, each with its own window, and stop
	// early once every discovered server has answered.
	want := allIDs(sortedKeys(snap.Servers))
	// Stream and consumer raft groups are listed per asset account. The
	// accounts of the previous epoch are asked in the same pass; an account
	// that JSZ reveals for the first time gets a second pass, so an epoch
	// takes at most three collect windows and usually two.
	detail := append(append([]Endpoint{}, DetailEndpoints...), accountRaftz(c.raftAccounts)...)
	c.gatherAll(ctx, snap, detail, want)
	accounts := jetStreamAccounts(snap)
	newAccounts := without(accounts, c.raftAccounts)
	// Account details are looked up with ACCOUNTZ in the same window as
	// the second RAFTZ pass, so the epoch takes no longer.
	lookUp := accountsToLookUp(snap, c.accountAsked, snap.StartedAt)
	var found []AccountDetails
	var lookups sync.WaitGroup
	lookups.Go(func() { found = c.lookUpAccounts(ctx, lookUp, want) })
	c.gatherAll(ctx, snap, accountRaftz(newAccounts), want)
	lookups.Wait()
	for _, id := range lookUp {
		c.accountAsked[id] = snap.StartedAt
	}
	for _, d := range found {
		if d.Name != "" && d.Name != d.Account {
			c.accountNames[d.Account] = d.Name
		}
	}
	snap.AccountDetails = found
	snap.AccountNames = c.listedAccountNames(snap)
	c.logUndecodedJWTs(snap)
	asked := append(append([]Endpoint{Statsz}, detail...), accountRaftz(newAccounts)...)
	if allJsz(snap) {
		// Every server answered JSZ, so an account of the previous epoch
		// with no streams now is gone: its request is dropped from this
		// epoch, so it is never recorded as missing, and not asked again.
		gone := accountRaftz(without(c.raftAccounts, accounts))
		for _, e := range gone {
			snap.forget(e)
		}
		asked = slices.DeleteFunc(asked, func(e Endpoint) bool { return slices.Contains(gone, e) })
		c.raftAccounts = accounts
	} else {
		// A server that did not answer JSZ may hold the account's only
		// streams: the previous accounts are unknown, not gone.
		c.raftAccounts = union(c.raftAccounts, accounts)
	}
	snap.markMissing(asked)
	snap.Expected = expectedNames(known, snap)
	c.tracker.observe(snap)
	snap.FinishedAt = c.now().UTC()
	return snap
}

// listedAccountNames returns the looked-up names of the accounts ACCOUNTZ
// lists in this snapshot.
func (c *Collector) listedAccountNames(snap *Snapshot) map[string]string {
	out := map[string]string{}
	for _, v := range snap.Servers {
		if v.Accountz == nil {
			continue
		}
		for _, id := range v.Accountz.Accounts {
			if name := c.accountNames[id]; name != "" {
				out[id] = name
			}
		}
	}
	return out
}

// gatherAll requests every endpoint in parallel and applies the replies.
func (c *Collector) gatherAll(ctx context.Context, snap *Snapshot, endpoints []Endpoint, want until) {
	replies := make([][][]byte, len(endpoints))
	errs := make([]error, len(endpoints))
	var wg sync.WaitGroup
	for i, e := range endpoints {
		wg.Go(func() {
			replies[i], errs[i] = gather(ctx, c.conn(), e.Subject(), e.requestBody(c.connzLimit), c.window, want)
		})
	}
	wg.Wait()
	for i, e := range endpoints {
		if errs[i] != nil {
			snap.Errors = append(snap.Errors, errs[i].Error())
		}
		c.applyAll(snap, e, replies[i])
	}
}

// jetStreamAccounts returns the accounts that any server reports
// JetStream streams for, sorted.
func jetStreamAccounts(snap *Snapshot) []string {
	set := map[string]bool{}
	for _, v := range snap.Servers {
		if v.Jsz == nil {
			continue
		}
		for _, a := range v.Jsz.AccountDetails {
			if a != nil && a.Id != "" && len(a.Streams) > 0 {
				set[a.Id] = true
			}
		}
	}
	return sortedKeys(set)
}

// allJsz reports whether at least one server answered and every server
// that answered anything answered JSZ with data in this epoch.
func allJsz(snap *Snapshot) bool {
	if len(snap.Servers) == 0 {
		return false
	}
	for _, v := range snap.Servers {
		if v.Jsz == nil {
			return false
		}
	}
	return true
}

// union returns the sorted distinct elements of a and b.
func union(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range append(append([]string{}, a...), b...) {
		set[x] = true
	}
	return sortedKeys(set)
}

// accountRaftz returns one RAFTZ request per account.
func accountRaftz(accounts []string) []Endpoint {
	out := make([]Endpoint, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, AccountRaftz(a))
	}
	return out
}

// without returns the elements of a not in b.
func without(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func (c *Collector) request(ctx context.Context, snap *Snapshot, e Endpoint, want until) {
	msgs, err := gather(ctx, c.conn(), e.Subject(), e.requestBody(c.connzLimit), c.window, want)
	if err != nil {
		snap.Errors = append(snap.Errors, err.Error())
	}
	c.applyAll(snap, e, msgs)
}

func (c *Collector) applyAll(snap *Snapshot, e Endpoint, msgs [][]byte) {
	for _, b := range msgs {
		if err := snap.Apply(e, b); err != nil {
			msg := fmt.Sprintf("%s: %v", e, err)
			c.log.Warn("undecodable reply", "endpoint", string(e), "err", err)
			snap.Errors = append(snap.Errors, msg)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
