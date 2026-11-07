package storeprov

import (
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

// Window and lane kinds, shared with the demo (see internal/api).
var (
	k8sLaneKinds = api.K8sLaneKinds
	windowKinds  = api.K8sWindowKinds
)

// upgradeGap is api.K8sUpgradeGap; upgradeLookback is how long before a
// range a window that reaches into it may have started.
const (
	upgradeGap      = api.K8sUpgradeGap
	upgradeLookback = 12 * time.Hour
)

// windowEventLimit bounds the events read to find the windows; the newest
// are kept. A variable so that tests can lower it.
var windowEventLimit = 5000

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
	rows, err := p.db().QueryContext(ctx, `SELECT `+eventCols+`, e.type, `+nodeOfEvent("e.")+` FROM events e WHERE `+where+
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

// k8sShortLabel is api.K8sShortLabel.
func k8sShortLabel(ev *v1.Event, typ, node string) string { return api.K8sShortLabel(ev, typ, node) }

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
SELECT time, kind, server_name, `+nodeOfEvent("")+`, json_extract_string(raw, '$.kubelet_version'),
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

// deriveWindows groups time-ordered events into windows, by
// api.DeriveK8sWindows.
func deriveWindows(evs []k8sEvent, gap time.Duration) []*v1.TimeWindow {
	out := make([]api.K8sWindowEvent, len(evs))
	for i, e := range evs {
		out[i] = api.K8sWindowEvent{At: e.at, Kind: e.kind, Server: e.server, Node: e.node, Version: e.version, Cause: e.cause, Pool: e.pool}
	}
	return api.DeriveK8sWindows(out, gap)
}
