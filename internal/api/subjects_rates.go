package api

import (
	"cmp"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Delivered rate limits and rules.
const (
	// DefaultSubscriptionsInterval is subscriptions_interval's default: a
	// provider that does not know the collector's uses it.
	DefaultSubscriptionsInterval = 5 * time.Minute
	// RateIntervals is how many subscription intervals two lists may lie
	// apart and still give a delivered rate.
	RateIntervals = 3
	// MaxConnectionRates is the most connections a row or match lists
	// with their rates.
	MaxConnectionRates = 20
)

// RateGap is the longest time between two subscription lists of a server
// that gives a delivered rate, for a collector's subscriptions_interval
// (DefaultSubscriptionsInterval when 0).
func RateGap(interval time.Duration) time.Duration {
	return RateIntervals * cmp.Or(interval, DefaultSubscriptionsInterval)
}

// ServerList is one server's subscription list: when the collection that
// asked for it started, and whether it holds every subscription (the
// server answered and the list was not cut at subsz_limit).
type ServerList struct {
	At       time.Time
	Complete bool
}

// subKey is a stored subscription row: server, account, subject, queue and
// connection.
type subKey struct {
	server, account, subject, queue string
	cid                             uint64
}

func keyOf(s SubscriptionFact) subKey { return subKey{s.Server, s.Account, s.Subject, s.Queue, s.CID} }

// DeliveredRates sets PerSecond on each of cur's subscriptions from the
// server's list before it (prev, with each server's newest earlier list):
// the increase of Msgs per second between the two. A subscription has
// none when its server's lists are not both complete, lie more than
// maxGap apart or not in order, it is not in the earlier list (same
// server, account, subject, queue and connection ID), its folded count
// changed or its count went down. The store's SQL follows the same rules.
func DeliveredRates(cur, prev []SubscriptionFact, curLists, prevLists map[string]ServerList, maxGap time.Duration) []SubscriptionFact {
	before := make(map[subKey]SubscriptionFact, len(prev))
	for _, p := range prev {
		before[keyOf(p)] = p
	}
	out := slices.Clone(cur)
	for i, s := range out {
		out[i].PerSecond = nil
		c, p := curLists[s.Server], prevLists[s.Server]
		gap := c.At.Sub(p.At)
		if !c.Complete || !p.Complete || gap <= 0 || gap > maxGap {
			continue
		}
		b, ok := before[keyOf(s)]
		if !ok || b.Subs != s.Subs || s.Msgs < b.Msgs {
			continue
		}
		v := float64(s.Msgs-b.Msgs) * 1000 / float64(gap.Milliseconds())
		out[i].PerSecond = &v
	}
	return out
}

// ConnectionRate is one connection's delivered rate on a group's
// subscriptions; PerSecond is nil when any of them has none.
type ConnectionRate struct {
	Name, Server string
	CID          uint64
	PerSecond    *float64
}

// rateAcc gathers a group's subscriptions by connection.
type rateAcc struct {
	name    string
	sum     float64
	unknown bool
}

// addRate adds one subscription's rate to its connection in rates.
func addRate(rates map[connKey]*rateAcc, k connKey, name string, v *float64) {
	r := rates[k]
	if r == nil {
		r = &rateAcc{name: name}
		rates[k] = r
	}
	if v == nil {
		r.unknown = true
		return
	}
	r.sum += *v
}

// finishRates sets a group's rate facts from its connections: every
// connection's rate in order, the sum of the measured ones and their
// count.
func finishRates(g *SubscriptionGroup, rates map[connKey]*rateAcc) {
	for k, r := range rates {
		c := ConnectionRate{Name: r.name, Server: k.server, CID: k.cid}
		if !r.unknown {
			v := r.sum
			c.PerSecond = &v
			g.Measured++
			g.PerSecond = ptrSum(g.PerSecond, v)
		}
		g.Rates = append(g.Rates, c)
	}
	SortConnectionRates(g.Rates)
}

func ptrSum(p *float64, v float64) *float64 {
	if p == nil {
		return &v
	}
	s := *p + v
	return &s
}

// SortConnectionRates orders connections most first, those without a rate
// last, then by name, server and connection ID.
func SortConnectionRates(rs []ConnectionRate) {
	slices.SortFunc(rs, func(a, b ConnectionRate) int {
		ra, rb := a.PerSecond, b.PerSecond
		if ra != nil {
			v := -*ra
			ra = &v
		}
		if rb != nil {
			v := -*rb
			rb = &v
		}
		return cmp.Or(compareSoonest(ra, rb), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Server, b.Server), cmp.Compare(a.CID, b.CID))
	})
}

// rateFields are a group's rate facts as the API's fields: the sum, the
// measured connections, the first MaxConnectionRates connections and how
// many more there are of the group's connections.
func rateFields(g SubscriptionGroup) (*float64, int32, []*v1.ConnectionRate, int32) {
	rs := slices.Clone(g.Rates)
	SortConnectionRates(rs)
	rs = rs[:min(len(rs), MaxConnectionRates)]
	out := make([]*v1.ConnectionRate, 0, len(rs))
	for _, r := range rs {
		out = append(out, &v1.ConnectionRate{Name: r.Name, ServerName: r.Server, Cid: int64(r.CID), PerSecond: r.PerSecond})
	}
	return g.PerSecond, g.Measured, out, max(g.Connections-int32(len(out)), 0)
}
