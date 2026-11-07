package k8s

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
)

// upgradeReason matches the reasons of node Events that report an
// upgrade (GKE's NodeUpgrade, UpgradeStarted and the like).
var upgradeReason = regexp.MustCompile(`(?i)upgrad`)

// maxNote bounds the Event note kept in the summary.
const maxNote = 300

// eventCount is how often the Event has occurred: the series count when
// the reporter groups occurrences, else the count of the older API.
func eventCount(e *eventsv1.Event) int64 {
	if e.Series != nil && e.Series.Count > 0 {
		return int64(e.Series.Count)
	}
	if e.DeprecatedCount > 0 {
		return int64(e.DeprecatedCount)
	}
	return 1
}

// eventTime is when the Event last occurred.
func eventTime(e *eventsv1.Event) time.Time {
	switch {
	case e.Series != nil && !e.Series.LastObservedTime.IsZero():
		return e.Series.LastObservedTime.Time
	case !e.DeprecatedLastTimestamp.IsZero():
		return e.DeprecatedLastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

// containerOf returns the container a field path such as
// spec.containers{nats} names; empty for none.
func containerOf(fieldPath string) string {
	_, rest, ok := strings.Cut(fieldPath, "{")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "}")
	return name
}

// eventObserved handles one Event of the Event stream. A repeat with a
// count already recorded is skipped, so a relist, the watch after it and
// a restart of natslens (seen is seeded from the store) store each
// occurrence count once.
func (t *tracker) eventObserved(e *eventsv1.Event) {
	uid := string(e.UID)
	n := eventCount(e)
	if s, ok := t.seen[uid]; ok && s.count >= n {
		return
	}
	kind, server, keep := t.classify(e)
	if !keep {
		return
	}
	t.seen[uid] = seenEvent{count: n, at: t.now()}
	ref := e.Regarding
	note := e.Note
	if len(note) > maxNote {
		note = note[:maxNote] + "..."
	}
	raw := map[string]any{
		"uid": uid, "count": n, "namespace": e.Namespace, "reason": e.Reason, "note": e.Note, "type": e.Type,
		"object":               map[string]any{"kind": ref.Kind, "namespace": ref.Namespace, "name": ref.Name, "uid": string(ref.UID), "field_path": ref.FieldPath},
		"reporting_controller": e.ReportingController,
	}
	if ref.Kind == "Pod" {
		if p := t.pods[podKey(ref.Namespace, ref.Name)]; p != nil && p.node != "" {
			raw["node"] = p.node
		}
	}
	if ref.Kind == "Node" {
		// As the node watch writes them, so that a node's Events count
		// as that node's.
		raw["node"] = ref.Name
		if n := t.nodes[ref.Name]; n != nil {
			raw["pool"], raw["kubelet_version"] = n.pool, n.version
		}
	}
	what := strings.ToLower(ref.Kind) + " " + ref.Name
	if ref.Namespace != "" && ref.Kind != "Pod" && ref.Kind != "Node" {
		what = strings.ToLower(ref.Kind) + " " + ref.Namespace + "/" + ref.Name
	}
	if ref.Kind == "Pod" && server == "" {
		what = "pod " + ref.Namespace + "/" + ref.Name
	}
	summary := fmt.Sprintf("%s %s", what, e.Reason)
	if note != "" {
		summary += ": " + note
	}
	if n > 1 {
		summary += fmt.Sprintf(" (x%d)", n)
	}
	t.record(eventTime(e), kind, e.Reason, server, e.Reason, summary, raw)
	if kind == KindPodEvicted || kind == KindPodKilling {
		if p := t.pods[podKey(ref.Namespace, ref.Name)]; p != nil {
			if kind == KindPodEvicted {
				p.disrupted = true
			}
			t.podDisrupted(p)
		}
	}
}

// classify decides whether an Event is stored, under which kind, and for
// which NATS server.
func (t *tracker) classify(e *eventsv1.Event) (kind, server string, keep bool) {
	ref := e.Regarding
	warning := e.Type == corev1.EventTypeWarning
	switch ref.Kind {
	case "Pod":
		k := podKey(ref.Namespace, ref.Name)
		_, nats := t.natsPods[k]
		mapped, has := podEventKinds[e.Reason]
		if !nats {
			if !warning && !clientPodReasons[e.Reason] {
				return "", "", false
			}
			if has {
				return mapped, "", true
			}
			return KindEvent, "", true
		}
		if skippedPodReasons[e.Reason] {
			return "", "", false
		}
		p := t.pods[k]
		// The reloader sidecar starts and stops with the server; only the
		// server container's reports are kept.
		if c := containerOf(ref.FieldPath); c != "" && p != nil && p.container != "" && c != p.container {
			return "", "", false
		}
		if mapped == KindPodEvicted && p != nil && p.disrupted {
			return "", "", false
		}
		server = t.serverName(ref.Namespace, ref.Name)
		if has {
			return mapped, server, true
		}
		return KindEvent, server, true
	case "Node":
		n := t.nodes[ref.Name]
		if n == nil || !t.relevant(n) {
			return "", "", false
		}
		if upgradeReason.MatchString(e.Reason) {
			t.lastDisruption = t.now()
			return KindNodeUpgrade, "", true
		}
		if warning && t.natsNode(n) {
			return KindEvent, "", true
		}
		return "", "", false
	}
	if warning {
		return KindEvent, "", true
	}
	return "", "", false
}
