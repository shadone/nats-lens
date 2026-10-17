package events

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// Stream and consumer action advisories do not say who made the call; the
// API audit advisory of the same call does. The correlator joins them when
// they arrive within matchWithin of each other, in either order: an action
// waits up to holdFor for its audit. An audit older than the previous
// action on the same asset belongs to an earlier call, and an audit of a
// call that changed nothing (an idempotent create) has no action at all, so
// neither may name the client of a later action.
const (
	matchWithin = 2 * time.Second
	holdFor     = 2 * time.Second
)

// action identifies the asset change an API call or advisory is about.
// Consumer creates and updates share the op "consumer.upsert": the server
// sends one created advisory for both.
type action struct {
	account, op, stream, consumer string
}

// auditAction maps a successful mutating API call to the action it causes.
func auditAction(a *server.JSAPIAudit) (action, bool) {
	toks := strings.Split(a.Subject, ".")
	if len(toks) < 5 || toks[0] != "$JS" || toks[1] != "API" {
		return action{}, false
	}
	switch {
	case toks[2] == "STREAM" && len(toks) == 5:
		switch toks[3] {
		case "CREATE":
			return action{op: "stream.created", stream: toks[4]}, true
		case "UPDATE":
			return action{op: "stream.updated", stream: toks[4]}, true
		case "DELETE":
			return action{op: "stream.deleted", stream: toks[4]}, true
		}
	case toks[2] == "CONSUMER" && toks[3] == "CREATE":
		if len(toks) >= 6 {
			return action{op: "consumer.upsert", stream: toks[4], consumer: toks[5]}, true
		}
		// An ephemeral or named create without the name in the subject:
		// the response carries the name.
		var r struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(a.Response), &r) == nil && r.Name != "" {
			return action{op: "consumer.upsert", stream: toks[4], consumer: r.Name}, true
		}
	case toks[2] == "CONSUMER" && toks[3] == "DURABLE" && len(toks) == 7 && toks[4] == "CREATE":
		return action{op: "consumer.upsert", stream: toks[5], consumer: toks[6]}, true
	case toks[2] == "CONSUMER" && toks[3] == "DELETE" && len(toks) == 6:
		return action{op: "consumer.deleted", stream: toks[4], consumer: toks[5]}, true
	}
	return action{}, false
}

// eventAction returns the action an action advisory event reports.
func eventAction(account string, e *Event) (action, bool) {
	op := e.Kind
	switch e.Kind {
	case "stream.created", "stream.updated", "stream.deleted", "consumer.deleted":
	case "consumer.created", "consumer.updated":
		op = "consumer.upsert"
	default:
		return action{}, false
	}
	return action{account: account, op: op, stream: e.Stream, consumer: e.Consumer}, true
}

type heldEvent struct {
	ev    Event
	at    time.Time
	until time.Time
}

type heldAudit struct {
	client *server.ClientInfo
	at     time.Time
}

// correlator attaches the client of an API audit to the matching action
// event. It is used from one goroutine.
type correlator struct {
	held   map[action][]heldEvent
	audits map[action]heldAudit
	// lastAction is the receive time of the latest action per key.
	lastAction map[action]time.Time
}

func newCorrelator() *correlator {
	return &correlator{held: map[action][]heldEvent{}, audits: map[action]heldAudit{}, lastAction: map[action]time.Time{}}
}

// add takes one decoded event received in account at time at and returns
// the events ready to store. raw is the payload, needed to read an audit's
// client and response.
func (c *correlator) add(account string, ev Event, raw []byte, at time.Time) []Event {
	if ev.Kind == KindAPIAudit {
		var a server.JSAPIAudit
		if json.Unmarshal(raw, &a) != nil || apiError(a.Response) != "" || a.Client == nil {
			return []Event{ev}
		}
		act, ok := auditAction(&a)
		if !ok {
			return []Event{ev}
		}
		act.account = account
		out := []Event{ev}
		if held := c.held[act]; len(held) > 0 && within(held[0].at, at) && !at.Before(c.previousAction(act, held[0].at)) {
			first := held[0].ev
			first.attachClient(a.Client)
			out = append(out, first)
			if len(held) == 1 {
				delete(c.held, act)
			} else {
				c.held[act] = held[1:]
			}
			return out
		}
		c.audits[act] = heldAudit{client: a.Client, at: at}
		return out
	}
	act, ok := eventAction(account, &ev)
	if !ok {
		return []Event{ev}
	}
	prev := c.lastAction[act]
	c.lastAction[act] = at
	if a, ok := c.audits[act]; ok {
		delete(c.audits, act)
		if within(a.at, at) && !a.at.Before(prev) {
			ev.attachClient(a.client)
			return []Event{ev}
		}
	}
	c.held[act] = append(c.held[act], heldEvent{ev: ev, at: at, until: at.Add(holdFor)})
	return nil
}

// previousAction returns the time of the action on key before the held one
// received at heldAt, zero when there was none.
func (c *correlator) previousAction(key action, heldAt time.Time) time.Time {
	if last := c.lastAction[key]; last.Before(heldAt) {
		return last
	}
	return time.Time{}
}

// within reports whether two receive times are close enough to belong to
// one API call.
func within(a, b time.Time) bool {
	d := a.Sub(b)
	return d <= matchWithin && d >= -matchWithin
}

// expire returns held action events whose audit did not come by now, and
// forgets audits and action times too old to match anything.
func (c *correlator) expire(now time.Time) []Event {
	var out []Event
	for k, held := range c.held {
		keep := held[:0]
		for _, h := range held {
			if now.Before(h.until) {
				keep = append(keep, h)
			} else {
				out = append(out, h.ev)
			}
		}
		if len(keep) == 0 {
			delete(c.held, k)
		} else {
			c.held[k] = keep
		}
	}
	for k, a := range c.audits {
		if now.Sub(a.at) > matchWithin {
			delete(c.audits, k)
		}
	}
	for k, at := range c.lastAction {
		if now.Sub(at) > time.Minute {
			delete(c.lastAction, k)
		}
	}
	return out
}

// flush returns every held event, for shutdown.
func (c *correlator) flush() []Event {
	var out []Event
	for k, held := range c.held {
		for _, h := range held {
			out = append(out, h.ev)
		}
		delete(c.held, k)
	}
	return out
}

// attachClient fills the client fields and names the client in the summary.
func (e *Event) attachClient(ci *server.ClientInfo) {
	e.setClient(ci, "")
	e.Summary += " by " + auditClient(ci)
}
