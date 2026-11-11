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

// duplicateSubscriptions are the client-009 findings of the newest
// subscription collection, as the check's query writes them: by
// application, its connections whose folded subscription row holds a
// subject and queue more than once, _INBOX. subjects and the servers' own
// left out. since is when the condition began, in the scenario.
func (s scen) duplicateSubscriptions(since time.Time) []*v1.Finding {
	c, ok := s.subsCollection(s.lastEpoch())
	if !ok {
		return nil
	}
	subs, conns := s.subscriptionsAt(c)
	type item struct {
		subject, queue string
		conns          int
		lo, hi         int64
	}
	type app struct {
		conns   map[[2]string]bool
		servers map[string]bool
		items   map[[2]string]*item
	}
	apps := map[string]*app{}
	for _, x := range subs {
		name, client := conns[x.Server].Names[x.CID]
		if !client || x.Subs < 2 || api.IsInbox(x.Subject) {
			continue
		}
		key := cmp.Or(strings.SplitN(name, "@", 2)[0], "(unnamed)")
		a := apps[key]
		if a == nil {
			a = &app{conns: map[[2]string]bool{}, servers: map[string]bool{}, items: map[[2]string]*item{}}
			apps[key] = a
		}
		a.conns[[2]string{x.Server, fmt.Sprint(x.CID)}] = true
		a.servers[x.Server] = true
		k := [2]string{x.Subject, x.Queue}
		it := a.items[k]
		if it == nil {
			it = &item{subject: x.Subject, queue: x.Queue, lo: x.Subs, hi: x.Subs}
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
			return cmp.Or(cmp.Compare(y.conns, x.conns), cmp.Compare(x.subject, y.subject), cmp.Compare(x.queue, y.queue))
		})
		var texts []string
		for _, it := range items {
			t := it.subject
			if it.queue != "" {
				t += " in queue group " + it.queue
			}
			t += " "
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
		detail += " a subscription more than once in the subscription lists of " + c.UTC().Format("15:04:05") + " UTC: " +
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
			Basis: "connections of the application in the newest subscription collection within the lookback that hold a " +
				"subscription with the same subject and queue group more than once"}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b *v1.Finding) int { return cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()) })
	return out
}
