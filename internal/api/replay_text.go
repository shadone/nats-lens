package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// clockText is a time of day in UTC, for example 09:18:30.
func clockText(t time.Time) string { return t.UTC().Format("15:04:05") }

// spanText is a span as hours and minutes, for example 09:12–09:18.
func spanText(a, b time.Time) string {
	return a.UTC().Format("15:04") + "–" + b.UTC().Format("15:04")
}

// durText is a duration in words, for example "40 s", "12 min" or
// "1 h 5 min".
func durText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Round(time.Second)/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Round(time.Minute)/time.Minute))
	}
	d = d.Round(time.Minute)
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	if m == 0 {
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

// entityLabel names an entity in a sentence: "restock-alerts on
// inventory-changes", "inventory-changes", "nats-1".
func entityLabel(e *v1.EntityRef) string {
	switch e.GetType() {
	case v1.EntityType_ENTITY_TYPE_CONSUMER:
		return e.GetConsumer() + " on " + e.GetStream()
	case v1.EntityType_ENTITY_TYPE_STREAM:
		return e.GetStream()
	case v1.EntityType_ENTITY_TYPE_SYSTEM:
		return "the system"
	}
	if e.GetName() != "" {
		return e.GetName()
	}
	return e.GetKey()
}

// entityNoun is the noun for an entity type: stream, consumer, server.
func entityNoun(t v1.EntityType) string {
	switch t {
	case v1.EntityType_ENTITY_TYPE_STREAM:
		return "stream"
	case v1.EntityType_ENTITY_TYPE_CONSUMER:
		return "consumer"
	case v1.EntityType_ENTITY_TYPE_SERVER:
		return "server"
	case v1.EntityType_ENTITY_TYPE_CLIENT:
		return "client"
	case v1.EntityType_ENTITY_TYPE_ACCOUNT:
		return "account"
	}
	return "entity"
}

// listText joins names as "a, b and c", naming at most three and counting
// the rest: "a, b, c and 4 more".
func listText(names []string) string {
	switch {
	case len(names) == 0:
		return ""
	case len(names) == 1:
		return names[0]
	case len(names) <= 3:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
	return strings.Join(names[:3], ", ") + fmt.Sprintf(" and %d more", len(names)-3)
}

// clientText names a client as "analytics-worker (10.20.0.12)".
func clientText(name, host string) string {
	switch {
	case name == "" && host == "":
		return ""
	case name == "":
		return "an unnamed client (" + host + ")"
	case host == "":
		return name
	}
	return name + " (" + host + ")"
}

// checkTitle is a check's name, or its code when the name is unknown.
func checkTitle(t ReplayTransition) string {
	if t.CheckName != "" {
		return t.CheckName
	}
	return t.Check
}

// stateWord names a transition's state in a label.
func stateWord(s v1.FindingState) string {
	switch s {
	case v1.FindingState_FINDING_STATE_FIRING:
		return "firing"
	case v1.FindingState_FINDING_STATE_CHANGED:
		return "changed"
	case v1.FindingState_FINDING_STATE_FLAPPING:
		return "flapping"
	case v1.FindingState_FINDING_STATE_RESOLVED:
		return "resolved"
	}
	return "transition"
}

// rawNode is the node an event's payload names, empty when none.
func rawNode(e *v1.Event) string {
	if e.GetRaw() == "" {
		return ""
	}
	var m struct {
		Node string `json:"node"`
	}
	if json.Unmarshal([]byte(e.GetRaw()), &m) != nil {
		return ""
	}
	return m.Node
}

// k8sTitle is the title of one Kubernetes event's step.
func k8sTitle(e *v1.Event) string {
	node, server := rawNode(e), e.GetServer()
	switch e.GetKind() {
	case "k8s.node.drained":
		if node != "" {
			return "Node " + node + " drained"
		}
	case "k8s.node.upgrade":
		if node != "" {
			return "Node " + node + " upgraded"
		}
	case "k8s.pod.failed_scheduling":
		return "The pod of " + server + " cannot be scheduled"
	case "k8s.pod.backoff":
		return "Kubernetes backs off restarting the pod of " + server
	case "k8s.pod.oom_killed":
		return "The pod of " + server + " is killed for memory (OOMKilled)"
	case "k8s.pod.killing":
		return "Kubernetes stops the pod of " + server
	case "k8s.pod.started":
		return "The pod of " + server + " starts"
	}
	return capitalize(e.GetSummary())
}

// k8sFoldTitle is the title of n folded Kubernetes events of one kind.
func k8sFoldTitle(first *v1.Event, n int, a, b time.Time) string {
	switch first.GetKind() {
	case "k8s.node.drained":
		return Plural(n, "node") + " drained (" + spanText(a, b) + ")"
	case "k8s.node.upgrade":
		return Plural(n, "node upgrade event") + " (" + spanText(a, b) + ")"
	}
	return fmt.Sprintf("%s (%d times, %s)", k8sTitle(first), n, spanText(a, b))
}

// announcementText says how a server announced it stopped, or that no
// announcement was captured; empty when events are not captured.
func announcementText(ann *v1.Event, captured bool) string {
	switch {
	case ann != nil && ann.GetKind() == "server.lameduck":
		return "Lame duck mode announced at " + clockText(ann.GetTime().AsTime()) + "."
	case ann != nil:
		return "Shutdown announced at " + clockText(ann.GetTime().AsTime()) + "."
	case captured:
		return "No lame duck or shutdown event was captured."
	}
	return ""
}

// assetsAfterText says what a restarted server held when it answered
// again, against what it held before: "Back at 09:18:30 with 4 streams and
// 9 consumers, 2 consumers fewer than before the restart."
func assetsAfterText(r ReplayRestart) string {
	out := "Back at " + clockText(r.FirstSeen)
	if r.StreamsAfter == nil || r.ConsumersAfter == nil {
		return out + "."
	}
	out += " with " + Plural(int(*r.StreamsAfter), "stream") + " and " + Plural(int(*r.ConsumersAfter), "consumer")
	if r.StreamsBefore == nil || r.ConsumersBefore == nil {
		return out + "."
	}
	var diff []string
	for _, d := range []struct {
		n    int32
		noun string
	}{{*r.StreamsAfter - *r.StreamsBefore, "stream"}, {*r.ConsumersAfter - *r.ConsumersBefore, "consumer"}} {
		switch {
		case d.n < 0:
			diff = append(diff, Plural(int(-d.n), d.noun)+" fewer")
		case d.n > 0:
			diff = append(diff, Plural(int(d.n), d.noun)+" more")
		}
	}
	if len(diff) == 0 {
		return out + ", as before the restart."
	}
	return out + ", " + strings.Join(diff, " and ") + " than before the restart."
}

// joinSentences joins the non-empty sentences with spaces.
func joinSentences(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// capitalize upper-cases the first letter of a sentence.
func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
