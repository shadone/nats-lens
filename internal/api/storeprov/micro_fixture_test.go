package storeprov_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/store"
)

// The micro conformance store holds 30 hours of epochs, 5 minutes apart,
// ending at last; subscription lists were collected in every epoch of the
// last 3 hours.
const (
	mcEpochs  = 30*12 + 1
	mcStep    = 5 * time.Minute
	mcSubsFor = 3 * time.Hour
)

// mcInstance is a service instance: where it runs, from when it answers
// and until when (zero: always, still), and a window in which it hangs,
// registered without answering.
type mcInstance struct {
	account, service, id, version, desc string
	server                              string
	cid                                 int64
	conn                                string
	started, from, to                   time.Time
	hungFrom, hungTo                    time.Time
	// reset is when its stats were reset: a new start time, counters from
	// 0, the same ID.
	reset time.Time
	// fall is when its counters fell without a new start time.
	fall time.Time
	// silent instances never answer $SRV requests.
	silent bool
}

// mcEndpoint is an endpoint of a service: per instance rate and average
// time at a time, error share, and a last error one instance reports.
type mcEndpoint struct {
	service, name, subject string
	rate                   float64
	ms                     func(t time.Time) float64
	errors                 float64
	lastError, lastBy      string
	// idleFrom and idleTo bound when it gets no request; zero: never.
	idleFrom, idleTo time.Time
}

// startedAt is the start time the instance reports at t.
func (in mcInstance) startedAt(t time.Time) time.Time {
	if !in.reset.IsZero() && !t.Before(in.reset) {
		return in.reset
	}
	return in.started
}

func (in mcInstance) runs(t time.Time) bool {
	return (in.from.IsZero() || !t.Before(in.from)) && (in.to.IsZero() || t.Before(in.to))
}

func (in mcInstance) answers(t time.Time) bool {
	return !in.silent && in.runs(t) && (in.hungFrom.IsZero() || t.Before(in.hungFrom) || !t.Before(in.hungTo))
}

// mcFixture is the store and what was written to it: each endpoint's
// reports by service and endpoint, as api.MicroReport.
type mcFixture struct {
	st      *store.Store
	last    time.Time
	reports map[string][]api.MicroReport
	// hours are each endpoint's hourly averages as micro_endpoint_hours
	// holds them, in hour order.
	hours map[string][]api.MicroAverage
	insts []mcInstance
}

// microConfStore writes the fixture. APP is asked: orders (O1 on nats-0,
// O2 on nats-1, 1.4.2), whose get endpoint takes 4 ms (varying by the
// hour) and 40 ms for 10 minutes 3 hours before last and from an hour
// before last on, and whose create endpoint
// fails 1% with O2's last error; pricing (P1, P2 on 2.0.0, P3 on 1.9.3),
// whose P2 was replaced by P4, started 12 minutes before last on the same
// connection while P2 answered until 3 minutes before last (P4's first
// report without a subject), and whose P1's
// counters fell 7 minutes before last without a new start time; inventory (I1, I2),
// whose I2 hung from 130 to 100 minutes before last, still subscribed,
// and whose I1's stats were reset 7 minutes before last;
// labels, subscribed and never answering. SUPPLIERS is asked and refused,
// every other epoch, its requests held in between.
// search (S1) is idle from 4 hours to an hour before last, then slow.
// PROD is not asked: catalog, seen only. APP's requests an hour before
// last timed out, its instances still subscribed.
func microConfStore(t *testing.T, last time.Time) *mcFixture {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	at := func(e int64) time.Time { return last.Add(-time.Duration(mcEpochs-e) * mcStep) }
	first := at(1)
	old := first.Add(-4 * time.Hour)
	insts := []mcInstance{
		{account: "APP", service: "orders", id: "O1", version: "1.4.2", desc: "Order intake and lookup", server: "S0", cid: 5, conn: "order-service", started: old},
		{account: "APP", service: "orders", id: "O2", version: "1.4.2", desc: "Order intake and lookup", server: "S1", cid: 7, conn: "order-service", started: old},
		{account: "APP", service: "pricing", id: "P1", version: "2.0.0", desc: "Price quotes", server: "S0", cid: 6, conn: "checkout-service", started: old,
			fall: last.Add(-7 * time.Minute)},
		{account: "APP", service: "pricing", id: "P2", version: "2.0.0", desc: "Price quotes", server: "S1", cid: 8, conn: "checkout-service", started: old,
			to: last.Add(-3 * time.Minute)},
		{account: "APP", service: "pricing", id: "P3", version: "1.9.3", desc: "Price quotes", server: "S1", cid: 10, conn: "checkout-service", started: old},
		{account: "APP", service: "pricing", id: "P4", version: "2.0.0", desc: "Price quotes", server: "S1", cid: 8, conn: "checkout-service",
			started: last.Add(-12 * time.Minute), from: last.Add(-12 * time.Minute)},
		{account: "APP", service: "inventory", id: "I1", version: "0.9.1", desc: "Stock levels", server: "S0", cid: 12, conn: "inventory-service", started: old,
			reset: last.Add(-7 * time.Minute)},
		{account: "APP", service: "inventory", id: "I2", version: "0.9.1", desc: "Stock levels", server: "S1", cid: 13, conn: "inventory-service", started: old,
			hungFrom: last.Add(-130 * time.Minute), hungTo: last.Add(-100 * time.Minute)},
		{account: "APP", service: "search", id: "S1", version: "2.3.0", desc: "Product search", server: "S0", cid: 14, conn: "search-api",
			started: old},
		{account: "APP", service: "labels", id: "L1", server: "S1", cid: 9, conn: "label-printer", started: old, silent: true},
		{account: "PROD", service: "catalog", id: "C1", server: "S0", cid: 11, conn: "catalog-api", started: old, silent: true},
	}
	slowFrom := last.Add(-time.Hour)
	eps := []mcEndpoint{
		{service: "orders", name: "get", subject: "orders.get.*", rate: 10, ms: func(t time.Time) float64 {
			// Slow for 10 minutes 3 hours before last, and from an hour
			// before last on.
			if t.After(slowFrom) || (t.After(last.Add(-3*time.Hour)) && !t.After(last.Add(-170*time.Minute))) {
				return 40
			}
			return 4 + 0.1*float64(t.Hour()%5)
		}},
		{service: "orders", name: "create", subject: "orders.create", rate: 2, errors: 0.01, lastError: "409 duplicate order id", lastBy: "O2",
			ms: func(t time.Time) float64 { return 3 + 0.2*float64(t.Hour()%3) }},
		// A second create endpoint on another subject: a series of its own
		// within the endpoint.
		// It is slow from 3 hours to 150 minutes before last, so create is
		// slow then.
		{service: "orders", name: "create", subject: "orders.create.v2", rate: 1, ms: func(t time.Time) float64 {
			if t.After(last.Add(-3*time.Hour)) && !t.After(last.Add(-150*time.Minute)) {
				return 200
			}
			return 9
		}},
		// Idle from 4 hours to an hour before last, then slow: the slow
		// run's span crosses the idle hours.
		{service: "search", name: "query", subject: "search.query", rate: 1, idleFrom: last.Add(-4 * time.Hour), idleTo: last.Add(-time.Hour),
			ms: func(t time.Time) float64 {
				if t.After(last.Add(-time.Hour)) {
					return 40
				}
				return 2
			}},
		{service: "pricing", name: "quote", subject: "pricing.quote", rate: 20, errors: 0.001, ms: func(time.Time) float64 { return 2.1 }},
		{service: "inventory", name: "stock", subject: "inventory.stock.get", rate: 3, ms: func(time.Time) float64 { return 1.6 }},
	}
	for i, name := range []string{"nats-0", "nats-1"} {
		r.add("servers", map[string]any{"server_id": fmt.Sprintf("S%d", i), "server_name": name, "first_epoch": 1, "last_epoch": mcEpochs,
			"first_seen_at": first, "last_seen_at": last})
	}
	for _, a := range []struct {
		id, name string
		system   bool
	}{{"APP", "shop", false}, {"SUPPLIERS", "", false}, {"PROD", "", false}, {"SYS", "", true}} {
		r.add("accounts", map[string]any{"account": a.id, "account_name": a.name, "is_system": a.system, "first_epoch": 1, "last_epoch": mcEpochs})
	}
	f := &mcFixture{st: st, last: last, reports: map[string][]api.MicroReport{}, insts: insts}
	type counters struct {
		req, errs, ns float64
		at            time.Time
	}
	acc := map[string]*counters{}
	for e := int64(1); e <= mcEpochs; e++ {
		t0 := at(e)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 2, 2, [], [], [], 0, false, [], NULL)`, e, t0,
			t0.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		subs := last.Sub(t0) < mcSubsFor
		answered := map[string]int{}
		for _, in := range insts {
			if in.answers(t0) && !t0.Equal(last.Add(-time.Hour)) {
				answered[in.account]++
			}
		}
		for _, sid := range []string{"S0", "S1"} {
			r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": sid, "endpoint": "CONNZ", "state": "ok"})
			if subs {
				r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": sid, "endpoint": "SUBSZ", "state": "ok"})
				r.add("subscription_lists", map[string]any{"epoch": e, "server_id": sid, "total": 100, "listed": 100})
			}
		}
		timedOut := t0.Equal(last.Add(-time.Hour))
		for _, req := range []string{"INFO", "STATS"} {
			state := "ok"
			if timedOut {
				state = "timeout"
			}
			r.add("micro_requests", map[string]any{"epoch": e, "label": "app", "account": "APP", "request": req, "state": state,
				"replies": answered["APP"]})
			// Refused every other epoch and held in between, for the 10
			// minutes after the refusal.
			sup := map[string]any{"epoch": e, "label": "suppliers", "account": "SUPPLIERS", "request": req, "state": "error",
				"replies": 0, "error": `nats: Permissions Violation for Publish to "$SRV.` + req + `"`}
			if (mcEpochs-e)%2 == 0 && e > 1 {
				sup["state"], sup["refused_at"], sup["retry_after"] = "held", t0.Add(-mcStep), t0.Add(-mcStep).Add(10*time.Minute)
			}
			r.add("micro_requests", sup)
		}
		listed := map[int64]bool{}
		for _, in := range insts {
			if subs && in.runs(t0) {
				if !listed[in.cid] {
					listed[in.cid] = true
					r.add("connections", map[string]any{"epoch": e, "server_id": in.server, "cid": in.cid, "name": in.conn, "account": in.account})
				}
				sub := func(subject, queue string) {
					cols := map[string]any{"epoch": e, "server_id": in.server, "account": in.account, "subject": subject, "cid": in.cid,
						"subs": 1, "msgs": 0}
					if queue != "" {
						cols["queue"] = queue
					}
					r.add("subscriptions", cols)
				}
				for _, verb := range []string{"PING", "INFO", "STATS"} {
					sub("$SRV."+verb+"."+in.service+"."+in.id, "")
				}
				for _, ep := range eps {
					if ep.service == in.service {
						sub(ep.subject, "q")
					}
				}
			}
			// Counters run whether or not the instance answers.
			for _, ep := range eps {
				if ep.service != in.service || !in.runs(t0) || t0.Before(in.started) {
					continue
				}
				k := in.id + "/" + ep.name + "/" + ep.subject
				c := acc[k]
				if c == nil {
					c = &counters{at: in.started}
					acc[k] = c
				}
				if !in.reset.IsZero() && c.at.Before(in.reset) && !t0.Before(in.reset) {
					*c = counters{at: in.reset}
				}
				if !in.fall.IsZero() && c.at.Before(in.fall) && !t0.Before(in.fall) {
					*c = counters{at: in.fall}
				}
				dt := t0.Sub(c.at).Seconds()
				n := ep.rate * dt
				if !ep.idleFrom.IsZero() && t0.After(ep.idleFrom) && !t0.After(ep.idleTo) {
					n = 0
				}
				c.req += n
				c.errs += n * ep.errors
				c.ns += n * ep.ms(t0) * 1e6
				c.at = t0
			}
			if !in.answers(t0) || timedOut {
				continue
			}
			r.add("micro_instances", map[string]any{"epoch": e, "account": in.account, "service": in.service, "instance_id": in.id,
				"version": in.version, "description": in.desc, "endpoints": "[]"})
			r.add("micro_stats", map[string]any{"epoch": e, "account": in.account, "service": in.service, "instance_id": in.id,
				"version": in.version, "started": in.startedAt(t0)})
			for _, ep := range eps {
				if ep.service != in.service {
					continue
				}
				c := acc[in.id+"/"+ep.name+"/"+ep.subject]
				req, errs, ns := int64(math.Floor(c.req)), int64(math.Floor(c.errs)), int64(math.Floor(c.ns))
				cols := map[string]any{"epoch": e, "account": in.account, "service": in.service, "instance_id": in.id, "endpoint": ep.name,
					"subject": ep.subject, "queue_group": "q", "num_requests": req, "num_errors": errs, "processing_time_ns": ns,
					"average_processing_time_ns": ns / max(req, 1)}
				if ep.lastError != "" && ep.lastBy == in.id {
					cols["last_error"] = ep.lastError
				}
				if (in.service == "orders" && ep.name == "get" && t0.Equal(last.Add(-55*time.Minute))) ||
					(in.id == "P4" && t0.Equal(in.from.Add(2*time.Minute))) {
					// P4's first STATS came before INFO gave its endpoint a
					// subject, and both orders instances' get rows of the slow
					// run's first collection lack theirs: still one series.
					delete(cols, "subject")
				}
				r.add("micro_endpoints", cols)
				k := in.service + "/" + ep.name
				// One series per instance and subject.
				f.reports[k] = append(f.reports[k], api.MicroReport{Instance: in.id + "|" + ep.subject, Started: in.startedAt(t0), At: t0, Requests: req, Errors: errs,
					ProcessingNs: ns})
			}
		}
	}
	f.fillHours(t, at(1))
	return f
}

// fillHours writes micro_endpoint_hours as the store's writer fills it
// (store.microHoursQuery, which also takes an instance's rows once per
// epoch; the fixture writes each once)
// for each epoch: per endpoint and hour, each instance's increase over its
// previous report within the hour before; the whole value for a new run
// (a counter that fell, a new start time) or for an instance that started
// after the previous epoch; nothing for any other first report. hours
// keeps the result for the tests.
func (f *mcFixture) fillHours(t *testing.T, first time.Time) {
	t.Helper()
	type key struct {
		hour              time.Time
		service, endpoint string
	}
	type sum struct {
		req, errs, ns int64
		epochs        map[time.Time]bool
	}
	sums := map[key]*sum{}
	for k, reps := range f.reports {
		service, endpoint, _ := strings.Cut(k, "/")
		last := map[string]api.MicroReport{}
		for _, r := range reps {
			p, ok := last[r.Instance]
			if ok && r.At.Sub(p.At) > time.Hour {
				ok = false
			}
			var dr, de, dn int64
			switch {
			case ok && (r.Requests < p.Requests || r.Errors < p.Errors || r.ProcessingNs < p.ProcessingNs || !r.Started.Equal(p.Started)),
				!ok && r.At.After(first) && r.Started.After(r.At.Add(-mcStep)):
				dr, de, dn = r.Requests, r.Errors, r.ProcessingNs
			case ok:
				dr, de, dn = r.Requests-p.Requests, r.Errors-p.Errors, r.ProcessingNs-p.ProcessingNs
			}
			last[r.Instance] = r
			hk := key{r.At.Truncate(time.Hour), service, endpoint}
			if sums[hk] == nil {
				sums[hk] = &sum{epochs: map[time.Time]bool{}}
			}
			x := sums[hk]
			x.req, x.errs, x.ns = x.req+dr, x.errs+de, x.ns+dn
			x.epochs[r.At] = true
		}
	}
	f.hours = map[string][]api.MicroAverage{}
	r := rows{t, f.st}
	for k, x := range sums {
		r.add("micro_endpoint_hours", map[string]any{"hour": k.hour, "account": "APP", "service": k.service, "endpoint": k.endpoint,
			"requests": x.req, "errors": x.errs, "processing_time_ns": x.ns, "epochs": len(x.epochs)})
		if x.req > 0 {
			hk := k.service + "/" + k.endpoint
			f.hours[hk] = append(f.hours[hk], api.MicroAverage{At: k.hour, Ms: float64(x.ns) / float64(x.req) / 1e6})
		}
	}
	for k := range f.hours {
		slices.SortFunc(f.hours[k], func(a, b api.MicroAverage) int { return a.At.Compare(b.At) })
	}
}

func microConfProvider(t *testing.T, last time.Time) (*storeprov.Provider, *mcFixture) {
	t.Helper()
	f := microConfStore(t, last)
	clock := last.Add(30 * time.Second)
	return &storeprov.Provider{St: f.st, Interval: mcStep, Live: true, Clock: func() time.Time { return clock }, Retention: 14 * day}, f
}
