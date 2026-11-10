package store

import (
	"context"
	"fmt"
	"time"
)

// ClientPod is one pod of a NATS client as the Kubernetes watcher saw it.
type ClientPod struct {
	UID, Namespace, Name, Node string
	WorkloadKind, Workload     string
	AppName                    string
	IPs                        []string
	HostNetwork                bool
	// StartedAt is the pod's start time; zero when not known, and the
	// write's time is used.
	StartedAt time.Time
}

// ClientPodRelist is a (re)list of the client pods of one namespace
// (empty for every namespace): the live pods in it that it does not list
// ended at their last sighting.
type ClientPodRelist struct {
	Namespace string
	Listed    []string
}

// ClientPodBatch is what the watcher writes at once.
type ClientPodBatch struct {
	At time.Time
	// Seen are the pods that changed or were listed: their addresses are
	// written, and live again when they had ended.
	Seen []ClientPod
	// Ended are the UIDs of pods that released their addresses, with when.
	Ended map[string]time.Time
	// Relists are the lists since the last batch.
	Relists []ClientPodRelist
	// Touch refreshes last_seen of every live address to At.
	Touch bool
}

// WriteClientPods writes one batch of client pods in one transaction:
// the relists first, then the pods seen, the pods ended and the touch.
func (s *Store) WriteClientPods(ctx context.Context, b ClientPodBatch) error {
	if len(b.Seen) == 0 && len(b.Ended) == 0 && len(b.Relists) == 0 && !b.Touch {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	at := b.At.UTC()
	for _, r := range b.Relists {
		if _, err := tx.ExecContext(ctx, `
UPDATE client_pods SET ended_at = last_seen
WHERE ended_at IS NULL AND (? = '' OR namespace = ?) AND NOT list_contains(?::VARCHAR[], uid)`,
			r.Namespace, r.Namespace, append([]string{}, r.Listed...)); err != nil {
			return fmt.Errorf("client pods relist: %w", err)
		}
	}
	type key struct{ uid, ip string }
	done := map[key]bool{}
	for _, p := range b.Seen {
		first := p.StartedAt.UTC()
		if p.StartedAt.IsZero() || first.After(at) {
			first = at
		}
		for _, ip := range p.IPs {
			k := key{p.UID, ip}
			if ip == "" || done[k] {
				continue
			}
			done[k] = true
			if _, err := tx.ExecContext(ctx, `
INSERT INTO client_pods (uid, ip, namespace, pod, node, workload_kind, workload, app_name, host_network, first_seen, last_seen, ended_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT (uid, ip) DO UPDATE SET node = excluded.node, workload_kind = excluded.workload_kind, workload = excluded.workload,
  app_name = excluded.app_name, host_network = excluded.host_network, last_seen = excluded.last_seen, ended_at = NULL`,
				utf8(p.UID), utf8(ip), utf8(p.Namespace), utf8(p.Name), utf8(p.Node), utf8(p.WorkloadKind), utf8(p.Workload),
				utf8(p.AppName), p.HostNetwork, first, at); err != nil {
				return fmt.Errorf("client pod %s/%s: %w", p.Namespace, p.Name, err)
			}
		}
	}
	for uid, when := range b.Ended {
		if _, err := tx.ExecContext(ctx, `UPDATE client_pods SET ended_at = greatest(first_seen, make_timestamp(?::BIGINT)),
  last_seen = greatest(last_seen, make_timestamp(?::BIGINT))
WHERE uid = ? AND ended_at IS NULL`, when.UnixMicro(), when.UnixMicro(), uid); err != nil {
			return fmt.Errorf("client pod end: %w", err)
		}
	}
	if b.Touch {
		if _, err := tx.ExecContext(ctx, `UPDATE client_pods SET last_seen = make_timestamp(?::BIGINT) WHERE ended_at IS NULL AND last_seen < make_timestamp(?::BIGINT)`,
			at.UnixMicro(), at.UnixMicro()); err != nil {
			return fmt.Errorf("client pods touch: %w", err)
		}
	}
	return tx.Commit()
}
