package fake

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// dupLookback is client-009's lookback.
const dupLookback = 30 * time.Minute

// newestLists are the subscriptions of each server's newest list within
// dupLookback before the newest epoch, as client-009 reads them, with the
// connections of the same collections and when each server's list was
// collected, by server.
func (s scen) newestLists() ([]api.SubscriptionFact, map[string]api.ServerConnections, map[string]time.Time) {
	c, ok := s.subsCollection(s.lastEpoch())
	if !ok {
		return nil, nil, nil
	}
	var out []api.SubscriptionFact
	conns := map[string]api.ServerConnections{}
	seen := map[string]time.Time{}
	// A collection's subscriptions are built only when a server not yet
	// read answered it.
	for at := c; !at.Before(s.lastEpoch().Add(-dupLookback)) && !at.Before(s.dataStart()); at = at.Add(-subsInterval) {
		var subs []api.SubscriptionFact
		var cs map[string]api.ServerConnections
		for _, l := range s.subszServers(at) {
			if _, ok := seen[l.Name]; !l.Answered || ok {
				continue
			}
			if cs == nil {
				subs, cs = s.subscriptionsAt(at)
			}
			seen[l.Name] = at
			conns[l.Name] = cs[l.Name]
			for _, x := range subs {
				if x.Server == l.Name {
					out = append(out, x)
				}
			}
		}
	}
	return out, conns, seen
}

// duplicateSubscriptions are the client-009 findings of each server's
// newest subscription list, as the check's query writes them: by
// application, its connections whose folded subscription row holds a
// subject without a queue group more than once; _INBOX. and $SRV.
// subjects and the servers' own left out. since is when the condition
// began, in the scenario.
func (s scen) duplicateSubscriptions(since time.Time) []*v1.Finding {
	subs, conns, listAt := s.newestLists()
	type item struct {
		subject string
		conns   int
		lo, hi  int64
	}
	type app struct {
		at      time.Time
		conns   map[[2]string]bool
		servers map[string]bool
		items   map[[2]string]*item
	}
	apps := map[string]*app{}
	for _, x := range subs {
		name, client := conns[x.Server].Names[x.CID]
		if !client || x.Subs < 2 || x.Queue != "" || api.IsInbox(x.Subject) || strings.HasPrefix(x.Subject, api.SrvPrefix) {
			continue
		}
		key := cmp.Or(strings.SplitN(name, "@", 2)[0], "(unnamed)")
		a := apps[key]
		if a == nil {
			a = &app{conns: map[[2]string]bool{}, servers: map[string]bool{}, items: map[[2]string]*item{}}
			apps[key] = a
		}
		a.at = latest(a.at, listAt[x.Server])
		a.conns[[2]string{x.Server, fmt.Sprint(x.CID)}] = true
		a.servers[x.Server] = true
		k := [2]string{x.Subject}
		it := a.items[k]
		if it == nil {
			it = &item{subject: x.Subject, lo: x.Subs, hi: x.Subs}
			a.items[k] = it
		}
		it.conns++
		it.lo, it.hi = min(it.lo, x.Subs), max(it.hi, x.Subs)
	}
	var out []*v1.Finding
	for key, a := range apps {
		var items []*item
		for _, it := range a.items {
			items = append(items, it)
		}
		slices.SortFunc(items, func(x, y *item) int {
			return cmp.Or(cmp.Compare(y.conns, x.conns), cmp.Compare(x.subject, y.subject))
		})
		var texts []string
		for _, it := range items {
			t := it.subject + " "
			if it.lo != it.hi {
				t += "up to "
			}
			t += fmt.Sprintf("%d times on %d connection", it.hi, it.conns)
			if it.conns != 1 {
				t += "s"
			}
			texts = append(texts, t)
		}
		var servers []string
		for sv := range a.servers {
			servers = append(servers, sv)
		}
		slices.Sort(servers)
		n := len(a.conns)
		detail := fmt.Sprintf("%d connection", n)
		if n == 1 {
			detail += " holds"
		} else {
			detail += "s hold"
		}
		detail += " a subscription more than once in the subscription lists of " + a.at.UTC().Format("15:04:05") + " UTC: " +
			strings.Join(texts[:min(len(texts), 5)], "; ")
		if len(texts) > 5 {
			detail += fmt.Sprintf("; and %d more", len(texts)-5)
		}
		detail += " (" + strings.Join(servers[:min(len(servers), 5)], ", ")
		if len(servers) > 5 {
			detail += fmt.Sprintf(" and %d more", len(servers)-5)
		}
		detail += ")."
		f := finding("client-009", "Duplicate subscription", warning, firing, clientRef(key), since, since, detail)
		f.Value = &v1.FindingValue{Name: "connections with a duplicate subscription", Display: fmt.Sprint(n), Number: ptr(float64(n)),
			Basis: "connections of the application, in each server's newest subscription list within the lookback, that hold a " +
				"plain subscription on one subject more than once"}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b *v1.Finding) int { return cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()) })
	return out
}
