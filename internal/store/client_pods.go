package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"strings"
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

// ClientPodRelist is a complete (re)list of the client pods of one
// namespace (empty for every namespace): the live pods in it that it does
// not list end at their last sighting. A list cut short is not a relist.
type ClientPodRelist struct {
	Namespace string
	Listed    []string
}

// ClientPodBatch is what the watcher writes at once.
type ClientPodBatch struct {
	At time.Time
	// Seen are the pods that changed or were listed: their addresses are
	// written, and live again when they had ended. A live address that a
	// pod no longer has ends at At.
	Seen []ClientPod
	// Ended are the UIDs of pods that released their addresses, with when.
	Ended map[string]time.Time
	// Relists are the complete lists since the last batch.
	Relists []ClientPodRelist
	// Touch are the namespaces ("" for every namespace) whose live
	// addresses have their last sighting refreshed to At: those whose
	// watch is healthy.
	Touch []string
}

// clientPodChunk is how many addresses one staging INSERT carries.
const clientPodChunk = 500

// canonicalIP writes an address as CONNZ does: an IPv4-mapped IPv6
// address as plain IPv4.
func canonicalIP(s string) string {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap().String()
	}
	return s
}

// EndLiveClientPods ends every live client pod address at its last
// sighting. natslens calls it as its client pod watch starts: a pod still
// there is live again with the first list, while one in a namespace no
// longer watched, or whose watch never lists, stays ended instead of
// matching a reused address.
func (s *Store) EndLiveClientPods(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `UPDATE client_pods SET ended_at = last_seen WHERE ended_at IS NULL`); err != nil {
		return fmt.Errorf("end live client pods: %w", err)
	}
	return nil
}

// WriteClientPods writes one batch of client pods in one transaction:
// the pods ended first, so that a deletion keeps its time when a relist in
// the same batch leaves the pod out, then the relists, the pods seen, the
// pods ended again (those first written by the batch) and the touch.
func (s *Store) WriteClientPods(ctx context.Context, b ClientPodBatch) error {
	if len(b.Seen) == 0 && len(b.Ended) == 0 && len(b.Relists) == 0 && len(b.Touch) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	at := b.At.UTC().UnixMicro()
	if err := endClientPods(ctx, tx, b.Ended); err != nil {
		return err
	}
	for _, r := range b.Relists {
		if _, err := tx.ExecContext(ctx, `
UPDATE client_pods SET ended_at = last_seen
WHERE ended_at IS NULL AND (? = '' OR namespace = ?) AND uid NOT IN (SELECT unnest(?::VARCHAR[]))`,
			r.Namespace, r.Namespace, append([]string{}, r.Listed...)); err != nil {
			return fmt.Errorf("client pods relist: %w", err)
		}
	}
	if len(b.Seen) > 0 {
		if err := writeSeenPods(ctx, tx, b.Seen, b.At); err != nil {
			return err
		}
	}
	// Again for the pods first written by this batch: one that lived less
	// than a flush is seen and ended at once.
	if len(b.Seen) > 0 {
		if err := endClientPods(ctx, tx, b.Ended); err != nil {
			return err
		}
	}
	if len(b.Touch) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE client_pods SET last_seen = make_timestamp(?::BIGINT)
WHERE ended_at IS NULL AND last_seen < make_timestamp(?::BIGINT) AND (list_contains(?::VARCHAR[], '') OR list_contains(?::VARCHAR[], namespace))`,
			at, at, append([]string{}, b.Touch...), append([]string{}, b.Touch...)); err != nil {
			return fmt.Errorf("client pods touch: %w", err)
		}
	}
	return tx.Commit()
}

// writeSeenPods stages the pods' addresses in a temporary table with
// multi-row INSERTs, then ends the live addresses their pods no longer
// have and upserts the staged ones, one statement each. An address new to
// a pod already stored starts at the write's time, not at the pod's
// start. When a batch holds a pod twice its last sighting counts.
func writeSeenPods(ctx context.Context, tx *sql.Tx, seen []ClientPod, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `CREATE OR REPLACE TEMP TABLE client_pods_in (uid VARCHAR, ip VARCHAR, namespace VARCHAR,
  pod VARCHAR, node VARCHAR, workload_kind VARCHAR, workload VARCHAR, app_name VARCHAR, host_network BOOLEAN, started BIGINT)`); err != nil {
		return fmt.Errorf("stage client pods: %w", err)
	}
	defer func() { _, _ = tx.ExecContext(context.WithoutCancel(ctx), `DROP TABLE IF EXISTS client_pods_in`) }()
	atUs := at.UTC().UnixMicro()
	var args []any
	rows := 0
	flush := func() error {
		if rows == 0 {
			return nil
		}
		q := "INSERT INTO client_pods_in VALUES " + strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?), ", rows), ", ")
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("stage client pods: %w", err)
		}
		args, rows = args[:0], 0
		return nil
	}
	pods := map[string]bool{}
	for i := len(seen) - 1; i >= 0; i-- {
		p := seen[i]
		if pods[p.UID] {
			continue
		}
		pods[p.UID] = true
		started := atUs
		if !p.StartedAt.IsZero() && !p.StartedAt.After(at) {
			started = p.StartedAt.UTC().UnixMicro()
		}
		ips := map[string]bool{}
		for _, raw := range p.IPs {
			ip := canonicalIP(raw)
			if ip == "" || ips[ip] {
				continue
			}
			ips[ip] = true
			args = append(args, utf8(p.UID), utf8(ip), utf8(p.Namespace), utf8(p.Name), utf8(p.Node), utf8(p.WorkloadKind),
				utf8(p.Workload), utf8(p.AppName), p.HostNetwork, started)
			if rows++; rows == clientPodChunk {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE client_pods SET ended_at = greatest(first_seen, make_timestamp(?::BIGINT)), last_seen = greatest(last_seen, make_timestamp(?::BIGINT))
WHERE ended_at IS NULL AND uid IN (SELECT uid FROM client_pods_in)
  AND NOT EXISTS (SELECT 1 FROM client_pods_in s WHERE s.uid = client_pods.uid AND s.ip = client_pods.ip)`, atUs, atUs); err != nil {
		return fmt.Errorf("client pods moved: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO client_pods (uid, ip, namespace, pod, node, workload_kind, workload, app_name, host_network, first_seen, last_seen, ended_at)
SELECT s.uid, s.ip, s.namespace, s.pod, s.node, s.workload_kind, s.workload, s.app_name, s.host_network,
  make_timestamp(CASE WHEN s.uid IN (SELECT uid FROM client_pods) THEN ?::BIGINT ELSE s.started END), make_timestamp(?::BIGINT), NULL
FROM client_pods_in s
ON CONFLICT (uid, ip) DO UPDATE SET node = excluded.node, workload_kind = excluded.workload_kind, workload = excluded.workload,
  app_name = excluded.app_name, host_network = excluded.host_network, last_seen = excluded.last_seen, ended_at = NULL`, atUs, atUs); err != nil {
		return fmt.Errorf("client pods: %w", err)
	}
	return nil
}

// endClientPods ends the live addresses of the pods in ended at their end.
func endClientPods(ctx context.Context, tx *sql.Tx, ended map[string]time.Time) error {
	for uid, when := range ended {
		if _, err := tx.ExecContext(ctx, `UPDATE client_pods SET ended_at = greatest(first_seen, make_timestamp(?::BIGINT)),
  last_seen = greatest(last_seen, make_timestamp(?::BIGINT))
WHERE uid = ? AND ended_at IS NULL`, when.UnixMicro(), when.UnixMicro(), uid); err != nil {
			return fmt.Errorf("client pod end: %w", err)
		}
	}
	return nil
}
