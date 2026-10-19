package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// k8sLaneKinds are the Kubernetes events drawn on the timeline: node
// disruptions, and the NATS pods' stops, starts and readiness. Pod kinds
// are drawn only for NATS pods (with a server name).
var k8sLaneKinds = []string{
	"k8s.node.cordoned", "k8s.node.drained", "k8s.node.not_ready", "k8s.node.upgrade", "k8s.node.deleted",
	"k8s.pod.killing", "k8s.pod.evicted", "k8s.pod.terminating", "k8s.pod.started", "k8s.pod.ready", "k8s.pod.not_ready",
	"k8s.pod.restarted", "k8s.pod.scheduled", "k8s.pod.preempted", "k8s.pod.oom_killed",
	"collector.k8s_gap", "collector.k8s_not_watching",
}

// Node upgrade windows.
const (
	// upgradeGap is the longest quiet time inside one window: an event
	// later than this after the window's last one starts a new window.
	upgradeGap = 15 * time.Minute
	// upgradeLookback is how long before a range a window that reaches
	// into it may have started.
	upgradeLookback = 12 * time.Hour
)

// windowEventLimit bounds the events read to find the windows; the newest
// are kept. A variable so that tests can lower it.
var windowEventLimit = 5000

// windowKinds are the events a window is made of; windowStarts are the
// ones that open a window. A cordon alone, which the cluster autoscaler
// also makes, opens none: a NATS pod has to leave the node.
var (
	windowKinds = []string{
		"k8s.node.cordoned", "k8s.node.drained", "k8s.node.upgrade", "k8s.node.deleted", "k8s.node.added",
		"k8s.node.ready", "k8s.node.not_ready", "k8s.node.uncordoned",
		"k8s.pod.killing", "k8s.pod.evicted", "k8s.pod.terminating", "k8s.pod.deleted", "k8s.pod.scheduled",
		"k8s.pod.started", "k8s.pod.ready", "k8s.pod.not_ready", "k8s.pod.restarted",
	}
	windowStarts = map[string]bool{"k8s.node.drained": true, "k8s.node.upgrade": true}
	// podStops count as pod restarts in a window's detail.
	podStops = map[string]bool{"k8s.pod.killing": true, "k8s.pod.evicted": true, "k8s.pod.terminating": true, "k8s.pod.restarted": true}
)

// k8sEvent is a stored Kubernetes event as the windows need it.
type k8sEvent struct {
	at      time.Time
	kind    string
	server  string
	node    string
	version string
	// cause is a drain's cordon cause: unschedulable or autoscaler.
	cause string
	// pool is the node's GKE node pool (cloud.google.com/gke-nodepool);
	// empty on other platforms.
	pool string
}

// clientPodKinds are the failures of a NATS client's pod drawn on the
// lane: it cannot be scheduled, crashes in a loop or runs out of memory.
var clientPodKinds = []string{"k8s.pod.failed_scheduling", "k8s.pod.backoff", "k8s.pod.oom_killed"}

// k8sFilter selects the Kubernetes events of the lane: node kinds and
// collector watch gaps always, pod kinds of NATS pods, the failures of
// pods that are NATS clients, and for a server entity only that server's
// pods. A client pod is one of podNames (see clientPodNames), or one
// clientPods matches.
func k8sFilter(start, end time.Time, entity *v1.EntityRef, clientPods *regexp.Regexp, podNames []string) (string, []any) {
	pattern := ""
	if clientPods != nil {
		pattern = clientPods.String()
	}
	where := []string{"e.time >= ?", "e.time < ?",
		"(e.source = 'k8s' OR e.kind LIKE 'collector.k8s_%')",
		"((e.kind IN (?" + strings.Repeat(", ?", len(k8sLaneKinds)-1) + ") AND (e.kind NOT LIKE 'k8s.pod.%' OR e.server_name IS NOT NULL))" +
			" OR (e.kind IN (?" + strings.Repeat(", ?", len(clientPodKinds)-1) + ") AND e.server_name IS NULL AND (" +
			"list_contains(?::VARCHAR[], coalesce(json_extract_string(e.raw, '$.object.name'), ''))" +
			" OR (? <> '' AND regexp_matches(coalesce(json_extract_string(e.raw, '$.object.name'), ''), ?)))))"}
	args := []any{start, end}
	for _, k := range k8sLaneKinds {
		args = append(args, k)
	}
	for _, k := range clientPodKinds {
		args = append(args, k)
	}
	args = append(args, podNames, pattern, pattern)
	if entity.GetType() == v1.EntityType_ENTITY_TYPE_SERVER {
		where, args = append(where, "(e.kind NOT LIKE 'k8s.pod.%' OR e.server_name = ?)"), append(args, entity.GetKey())
	}
	return strings.Join(where, " AND "), args
}

// k8sLane reads the newest laneLimit Kubernetes lane events of the range,
// oldest first, and whether there were more. Entities other than a server see the whole lane: a node
// drain is context for every stream and consumer.
func (p *Provider) k8sLane(ctx context.Context, r *v1.TimeRange, entity *v1.EntityRef) ([]*v1.LaneEvent, bool, error) {
	start, end := span(r)
	pods, err := p.clientPodNames(ctx, start, end)
	if err != nil {
		return nil, false, err
	}
	where, args := k8sFilter(start, end, entity, p.ClientPods, pods)
	rows, err := p.db().QueryContext(ctx, `SELECT `+eventCols+`, e.type, `+strings.ReplaceAll(nodeOfEvent, "raw", "e.raw")+` FROM events e WHERE `+where+
		` ORDER BY e.time DESC, e.id DESC LIMIT ?`, append(args, laneLimit+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("kubernetes lane: %w", err)
	}
	defer rows.Close()
	var out []*v1.LaneEvent
	truncated := false
	for rows.Next() {
		if len(out) == laneLimit {
			truncated = true
			break
		}
		var typ, node sql.NullString
		ev, err := scanEvent(rows, &typ, &node)
		if err != nil {
			return nil, false, err
		}
		out = append(out, &v1.LaneEvent{Id: ev.GetId(), Time: ev.GetTime(), Source: ev.GetSource(), Kind: ev.GetKind(),
			ShortLabel: k8sShortLabel(ev, typ.String, node.String), Label: ev.GetSummary(), Severity: ev.GetSeverity(), EventId: ev.GetId()})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	slices.Reverse(out)
	return out, truncated, nil
}

// k8sShortLabel names a lane item briefly: "nats-0 Killing", or "node"
// and the node's last name segment.
func k8sShortLabel(ev *v1.Event, typ, node string) string {
	what := strings.TrimPrefix(strings.TrimPrefix(ev.GetKind(), "k8s."), "collector.")
	if ev.GetServer() != "" {
		return ev.GetServer() + " " + cmp.Or(typ, what)
	}
	if strings.HasPrefix(ev.GetKind(), "k8s.node.") {
		// GKE node names end in a short unique suffix: gke-pool-1-7f3a.
		if i := strings.LastIndex(node, "-"); i >= 0 && i < len(node)-1 {
			return "node " + node[i+1:]
		}
		return "node " + cmp.Or(node, strings.TrimPrefix(what, "node."))
	}
	return what
}

// k8sWindows derives the node upgrade windows that overlap the range from
// the stored Kubernetes events: a window opens at a node drain or upgrade
// and takes in every following node and NATS pod event that comes within
// upgradeGap of the one before, so it ends at the last node or pod back
// to ready. With upgrade evidence (a kubelet version change or an upgrade
// Event, or nodes of different versions) it is a node upgrade, named a GKE
// node upgrade when a node carries GKE's node pool label; else a node
// drain.
func (p *Provider) k8sWindows(ctx context.Context, r *v1.TimeRange) ([]*v1.TimeWindow, error) {
	start, end := span(r)
	args := []any{start.Add(-upgradeLookback), end.Add(upgradeGap)}
	for _, k := range windowKinds {
		args = append(args, k)
	}
	rows, err := p.db().QueryContext(ctx, `
SELECT time, kind, server_name, `+nodeOfEvent+`, json_extract_string(raw, '$.kubelet_version'),
  json_extract_string(raw, '$.cause'), json_extract_string(raw, '$.pool')
FROM events
WHERE source = 'k8s' AND time >= ? AND time < ? AND kind IN (?`+strings.Repeat(", ?", len(windowKinds)-1)+`)
  AND (kind NOT LIKE 'k8s.pod.%' OR server_name IS NOT NULL)
ORDER BY time DESC, id DESC LIMIT `+strconv.Itoa(windowEventLimit), args...)
	if err != nil {
		return nil, fmt.Errorf("kubernetes windows: %w", err)
	}
	defer rows.Close()
	var evs []k8sEvent
	for rows.Next() {
		var e k8sEvent
		var server, node, version, cause, pool sql.NullString
		if err := rows.Scan(&e.at, &e.kind, &server, &node, &version, &cause, &pool); err != nil {
			return nil, err
		}
		e.at, e.server, e.node, e.version, e.cause, e.pool = e.at.UTC(), server.String, node.String, version.String, cause.String, pool.String
		evs = append(evs, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Read newest first, so that the limit keeps the newest events.
	slices.Reverse(evs)
	var out []*v1.TimeWindow
	for _, w := range deriveWindows(evs, upgradeGap) {
		if w.GetStart().AsTime().Before(end) && !w.GetEnd().AsTime().Before(start) {
			out = append(out, w)
		}
	}
	return out, nil
}

// deriveWindows groups time-ordered events into windows.
func deriveWindows(evs []k8sEvent, gap time.Duration) []*v1.TimeWindow {
	var out []*v1.TimeWindow
	var cur []k8sEvent
	closeWindow := func() {
		if touchesNATS(cur) {
			out = append(out, describeWindow(cur))
		}
		cur = nil
	}
	for _, e := range evs {
		if len(cur) > 0 && e.at.Sub(cur[len(cur)-1].at) > gap {
			closeWindow()
		}
		if len(cur) == 0 && !windowStarts[e.kind] {
			continue
		}
		cur = append(cur, e)
	}
	closeWindow()
	return out
}

func describeWindow(evs []k8sEvent) *v1.TimeWindow {
	drained, stopped, versions := map[string]bool{}, map[string]bool{}, map[string]bool{}
	upgrade, gke := false, false
	autoscaler := 0
	for _, e := range evs {
		gke = gke || e.pool != ""
		switch {
		case e.kind == "k8s.node.drained":
			drained[e.node] = true
			if e.cause == "autoscaler" {
				autoscaler++
			}
		case e.kind == "k8s.node.upgrade":
			upgrade = true
		case podStops[e.kind]:
			stopped[e.server] = true
		}
		if strings.HasPrefix(e.kind, "k8s.node.") && e.version != "" {
			versions[e.version] = true
		}
	}
	label := "Node drain"
	switch {
	case (upgrade || len(versions) > 1) && gke:
		label = "GKE node upgrade"
	case upgrade || len(versions) > 1:
		label = "Node upgrade"
	case len(drained) > 0 && autoscaler == countKind(evs, "k8s.node.drained"):
		label = "Node removed by the autoscaler"
	}
	return &v1.TimeWindow{Start: ts(evs[0].at), End: ts(evs[len(evs)-1].at), Label: label,
		Detail: fmt.Sprintf("%s, %s", api.Plural(len(drained), "drain"), api.Plural(len(stopped), "pod restart"))}
}

// touchesNATS reports whether a window holds a drain or a NATS pod event:
// an upgrade of nodes without NATS pods is not shown.
func touchesNATS(evs []k8sEvent) bool {
	for _, e := range evs {
		if e.kind == "k8s.node.drained" || e.server != "" {
			return true
		}
	}
	return false
}

func countKind(evs []k8sEvent, kind string) int {
	n := 0
	for _, e := range evs {
		if e.kind == kind {
			n++
		}
	}
	return n
}
