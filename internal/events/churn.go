package events

import (
	"fmt"
	"sort"
	"time"
)

// KindClientChurn is the event that stands for the connect and disconnect
// pairs of one client name that closed themselves within a window.
const KindClientChurn = "client.churn"

// ChurnOptions configure the collapsing of short-lived connections.
type ChurnOptions struct {
	// Collapse turns collapsing on.
	Collapse bool
	// PairWithin is how long a connect waits for its disconnect; a
	// connect not closed by then is stored as it is.
	PairWithin time.Duration
	// Window is the span one summary event covers per client name.
	Window time.Duration
}

type pairKey struct {
	server string
	cid    uint64
}

type churnKey struct {
	account, name, host string
	window              int64
}

type churnCount struct {
	first   time.Time
	last    time.Time
	n       int
	servers map[string]int
}

// churn collapses a connect and its disconnect that closed as "Client
// Closed" within PairWithin into one count per client name and window; a
// summary event per name, client host and window replaces the pairs. A disconnect for
// any other reason is stored with its connect. Not safe for concurrent
// use; Capture's Run loop owns it.
type churn struct {
	o      ChurnOptions
	held   map[pairKey]Event
	counts map[churnKey]*churnCount
}

func newChurn(o ChurnOptions) *churn {
	if o.PairWithin <= 0 {
		o.PairWithin = 10 * time.Second
	}
	if o.Window <= 0 {
		o.Window = time.Minute
	}
	return &churn{o: o, held: map[pairKey]Event{}, counts: map[churnKey]*churnCount{}}
}

// add returns the events to store now.
func (c *churn) add(e Event) []Event {
	if !c.o.Collapse || e.Source != SourceSys || e.clientID == 0 {
		return []Event{e}
	}
	k := pairKey{e.ServerName, e.clientID}
	switch e.Kind {
	case "client.connect":
		c.held[k] = e
		return nil
	case "client.disconnect":
		conn, ok := c.held[k]
		if !ok {
			return []Event{e}
		}
		delete(c.held, k)
		if e.Reason != "Client Closed" || e.ReceivedAt.Sub(conn.ReceivedAt) > c.o.PairWithin {
			return []Event{conn, e}
		}
		ck := churnKey{account: e.Account, name: e.ClientName, host: e.ClientHost, window: e.ReceivedAt.UnixNano() / int64(c.o.Window)}
		cnt := c.counts[ck]
		if cnt == nil {
			cnt = &churnCount{first: conn.Time, servers: map[string]int{}}
			c.counts[ck] = cnt
		}
		cnt.n++
		cnt.last = e.Time
		cnt.servers[e.ServerName]++
		return nil
	}
	return []Event{e}
}

// expire returns the connects held longer than PairWithin and the summaries
// of the windows that ended before now.
func (c *churn) expire(now time.Time) []Event {
	var out []Event
	for k, e := range c.held {
		if now.Sub(e.ReceivedAt) > c.o.PairWithin {
			out = append(out, e)
			delete(c.held, k)
		}
	}
	sortEvents(out)
	cur := now.UnixNano() / int64(c.o.Window)
	return append(out, c.summaries(func(k churnKey) bool { return k.window < cur }, now)...)
}

// flush returns everything held, for shutdown.
func (c *churn) flush() []Event {
	var out []Event
	for k, e := range c.held {
		out = append(out, e)
		delete(c.held, k)
	}
	sortEvents(out)
	var last time.Time
	for _, cnt := range c.counts {
		if cnt.last.After(last) {
			last = cnt.last
		}
	}
	return append(out, c.summaries(func(churnKey) bool { return true }, last)...)
}

func (c *churn) summaries(due func(churnKey) bool, received time.Time) []Event {
	var keys []churnKey
	for k := range c.counts {
		if due(k) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].window != keys[j].window {
			return keys[i].window < keys[j].window
		}
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		return keys[i].host < keys[j].host
	})
	var out []Event
	for _, k := range keys {
		cnt := c.counts[k]
		delete(c.counts, k)
		name := k.name
		if name == "" {
			name = "(unnamed)"
		}
		start := time.Unix(0, k.window*int64(c.o.Window)).UTC()
		srv := ""
		if len(cnt.servers) == 1 {
			for s := range cnt.servers {
				srv = s
			}
		}
		out = append(out, Event{
			// Time is the first collapsed connect; ReceivedAt is when the
			// summary was made (the window's flush), which files it in the
			// archive and the checks' lookbacks.
			Time: cnt.first.UTC(), ReceivedAt: received.UTC(), Source: SourceSys, Kind: KindClientChurn,
			ServerName: srv, Account: k.account, ClientName: k.name, ClientHost: k.host, Reason: "Client Closed",
			Summary: fmt.Sprintf("%s connected and disconnected %d times in %s (each closed by the client within %s)",
				name, cnt.n, spanText(c.o.Window), spanText(c.o.PairWithin)),
			Raw: mustJSON(map[string]any{"client_name": k.name, "client_host": k.host, "connects": cnt.n, "disconnects": cnt.n,
				"servers": cnt.servers, "window_start": start, "window_end": start.Add(c.o.Window),
				"first": cnt.first.UTC(), "last": cnt.last.UTC()}),
		})
	}
	return out
}

func sortEvents(evs []Event) {
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].ReceivedAt.Before(evs[j].ReceivedAt) })
}

// spanText writes a duration as "1 min" or "10 s".
func spanText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d min", d/time.Minute)
	}
	return fmt.Sprintf("%g s", d.Seconds())
}
