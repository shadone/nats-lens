package k8s

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/events"
)

// How long the tracker remembers what it saw.
const (
	// natsNodeFor is how long a node counts as a NATS node after its last
	// NATS pod left it, so that its cordon, drain and deletion are kept.
	natsNodeFor = time.Hour
	// natsPoolFor is how long a node pool counts as holding NATS nodes, so
	// that the nodes an upgrade adds to it are kept.
	natsPoolFor = 24 * time.Hour
	// natsPodFor is how long Events of a deleted NATS pod are still
	// recognised as a NATS pod's.
	natsPodFor = time.Hour
	// seenFor is how long an Event's count is remembered; the API server
	// keeps an Event for an hour by default.
	seenFor = 3 * time.Hour
)

// tracker holds the last state of the watched objects and turns changes
// into events. One goroutine owns it.
type tracker struct {
	serverName func(ns, pod string) string
	// poolNodes keeps the changes of every node of a pool that held a
	// NATS node.
	poolNodes bool
	// lastDisruption is when the last drain or upgrade was recorded.
	lastDisruption time.Time
	// prior is the stored state the first lists are compared with.
	prior Prior
	now   func() time.Time
	emit  func(events.Event)

	// pods are the NATS pods by namespace/name.
	pods map[string]*podState
	// natsPods are NATS pods seen, by namespace/name, with when.
	natsPods map[string]time.Time
	nodes    map[string]*nodeState
	// pools are the node pools that held a NATS node, with when.
	pools map[string]time.Time
	// seen is the count of each Event recorded, by UID.
	seen map[string]seenEvent
}

type seenEvent struct {
	count int64
	at    time.Time
}

func newTracker(serverName func(ns, pod string) string, now func() time.Time, emit func(events.Event), seen map[string]int64) *tracker {
	t := &tracker{
		serverName: serverName, now: now, emit: emit,
		pods: map[string]*podState{}, natsPods: map[string]time.Time{}, nodes: map[string]*nodeState{},
		pools: map[string]time.Time{}, seen: map[string]seenEvent{},
	}
	at := now()
	for uid, n := range seen {
		t.seen[uid] = seenEvent{count: n, at: at}
	}
	return t
}

// serverNameFunc returns the mapping from a NATS pod to its server name
// for a template with {pod} and {namespace}.
func serverNameFunc(template string) func(ns, pod string) string {
	return func(ns, pod string) string {
		return strings.NewReplacer("{pod}", pod, "{namespace}", ns).Replace(template)
	}
}

// record emits one event with source k8s, received now.
func (t *tracker) record(at time.Time, kind, typ, server, reason, summary string, raw map[string]any) {
	now := t.now().UTC()
	if at.IsZero() {
		at = now
	}
	b := mustJSON(raw)
	t.emit(events.Event{
		Time: at.UTC(), ReceivedAt: now, Source: events.SourceK8s, Type: typ, Kind: kind,
		ServerName: server, Reason: reason, Summary: summary, Raw: b,
	})
}

// expire forgets what is older than its keep time.
func (t *tracker) expire() {
	now := t.now()
	for k, at := range t.natsPods {
		if _, live := t.pods[k]; !live && now.Sub(at) > natsPodFor {
			delete(t.natsPods, k)
		}
	}
	for k, at := range t.pools {
		if now.Sub(at) > natsPoolFor {
			delete(t.pools, k)
		}
	}
	for k, s := range t.seen {
		if now.Sub(s.at) > seenFor {
			delete(t.seen, k)
		}
	}
}

// mustJSON marshals values that cannot fail to marshal (maps of strings,
// numbers and times).
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
