package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// K8sEventCounts returns, per Kubernetes Event UID, the highest occurrence
// count stored for events received since since, so that a restarted
// watcher does not store the Events it lists again.
func (s *Store) K8sEventCounts(ctx context.Context, since time.Time) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT json_extract_string(raw, '$.uid'), max(CAST(json_extract(raw, '$.count') AS BIGINT))
FROM events
WHERE source = 'k8s' AND received_at >= ? AND json_extract(raw, '$.count') IS NOT NULL
GROUP BY 1`, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("kubernetes event counts: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var uid string
		var n int64
		if err := rows.Scan(&uid, &n); err != nil {
			return nil, err
		}
		out[uid] = n
	}
	return out, rows.Err()
}

// K8sObjectState is what the stored Kubernetes events last said of a pod
// or a node: its readiness (pods) or cordon (nodes), when one was
// recorded, and whether its last event was its deletion.
type K8sObjectState struct {
	// Flag is ready for a pod, cordoned for a node; nil when unknown.
	Flag    *bool
	Deleted bool
}

// K8sLastState reads, from the watcher's events received since since,
// the last state of every NATS pod (by namespace/name) and node, and the
// time the watcher was last known to run: the newer of the last
// Kubernetes event and the last epoch. That time is zero for an empty
// database.
func (s *Store) K8sLastState(ctx context.Context, since time.Time) (pods, nodes map[string]K8sObjectState, last time.Time, err error) {
	var lt *time.Time
	if err = s.db.QueryRowContext(ctx, `SELECT greatest(
    (SELECT max(finished_at) FROM epochs),
    (SELECT max(received_at) FROM events WHERE source = 'k8s'))`).Scan(&lt); err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("kubernetes last activity: %w", err)
	}
	if lt != nil {
		last = lt.UTC()
	}
	read := func(key, flagOn, flagOff, kinds string) (map[string]K8sObjectState, error) {
		rows, err := s.db.QueryContext(ctx, `
SELECT `+key+` AS k,
  arg_max(kind, time) FILTER (WHERE kind IN ('`+flagOn+`', '`+flagOff+`')),
  arg_max(kind, time)
FROM events
WHERE source = 'k8s' AND received_at >= ? AND kind IN (`+kinds+`) AND `+key+` IS NOT NULL
GROUP BY k`, since.UTC())
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]K8sObjectState{}
		for rows.Next() {
			var k string
			var flag, lastKind *string
			if err := rows.Scan(&k, &flag, &lastKind); err != nil {
				return nil, err
			}
			st := K8sObjectState{Deleted: lastKind != nil && strings.HasSuffix(*lastKind, ".deleted")}
			if flag != nil {
				on := *flag == flagOn
				st.Flag = &on
			}
			out[k] = st
		}
		return out, rows.Err()
	}
	pods, err = read(`json_extract_string(raw, '$.namespace') || '/' || json_extract_string(raw, '$.pod')`,
		"k8s.pod.ready", "k8s.pod.not_ready",
		`'k8s.pod.scheduled', 'k8s.pod.ready', 'k8s.pod.not_ready', 'k8s.pod.restarted', 'k8s.pod.evicted', 'k8s.pod.terminating', 'k8s.pod.deleted'`)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("kubernetes pod state: %w", err)
	}
	nodes, err = read(`json_extract_string(raw, '$.node')`, "k8s.node.cordoned", "k8s.node.uncordoned",
		`'k8s.node.added', 'k8s.node.cordoned', 'k8s.node.uncordoned', 'k8s.node.ready', 'k8s.node.not_ready', 'k8s.node.upgrade', 'k8s.node.deleted'`)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("kubernetes node state: %w", err)
	}
	return pods, nodes, last, nil
}
