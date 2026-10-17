package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replicaReport is what epoch's rows say of one replica of a stream: the
// server's own report, and the leader's view of it.
type replicaReport struct {
	reports, leader bool
	hash            string
	listed          bool // the leader lists the replica
	current         sql.NullBool
	offline         sql.NullBool
	lag             sql.NullInt64
}

// streamReplicas reads, for every stream of the epoch (optionally one
// account or stream), each replica's report keyed by server name.
// prefix keeps only the streams whose name starts with it (all when
// empty).
func (p *Provider) streamReplicas(ctx context.Context, epoch int64, account, stream, prefix string) (map[string]map[string]*replicaReport, error) {
	out := map[string]map[string]*replicaReport{}
	get := func(acc, st, server string) *replicaReport {
		key := acc + "/" + st
		if out[key] == nil {
			out[key] = map[string]*replicaReport{}
		}
		if out[key][server] == nil {
			out[key][server] = &replicaReport{}
		}
		return out[key][server]
	}
	rows, err := p.db().QueryContext(ctx, `
SELECT x.account, x.stream, s.server_name, coalesce(x.is_leader, false), coalesce(sc.config_hash, '')
FROM stream_stats x JOIN servers s USING (server_id)
LEFT JOIN stream_config sc ON sc.server_id = x.server_id AND sc.account = x.account AND sc.stream = x.stream
  AND x.epoch BETWEEN sc.first_epoch AND sc.last_epoch
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) AND starts_with(x.stream, ?)`,
		epoch, account, account, stream, stream, prefix)
	if err != nil {
		return nil, fmt.Errorf("stream replicas: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var acc, st, server, hash string
		var leader bool
		if err := rows.Scan(&acc, &st, &server, &leader, &hash); err != nil {
			return nil, err
		}
		r := get(acc, st, server)
		r.reports, r.leader, r.hash = true, r.leader || leader, hash
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	lead, err := p.db().QueryContext(ctx, `
SELECT rp.account, rp.stream, rp.peer_name, rp.current, rp.offline, rp.lag FROM stream_replicas rp
JOIN stream_stats l ON l.epoch = rp.epoch AND l.server_id = rp.server_id AND l.account = rp.account
  AND l.stream = rp.stream AND l.is_leader
WHERE rp.epoch = ? AND (? = '' OR rp.account = ?) AND (? = '' OR rp.stream = ?) AND starts_with(rp.stream, ?)`,
		epoch, account, account, stream, stream, prefix)
	if err != nil {
		return nil, fmt.Errorf("stream replicas: %w", err)
	}
	defer lead.Close()
	for lead.Next() {
		var acc, st, peer string
		var current, offline sql.NullBool
		var lag sql.NullInt64
		if err := lead.Scan(&acc, &st, &peer, &current, &offline, &lag); err != nil {
			return nil, err
		}
		r := get(acc, st, peer)
		r.listed, r.current, r.offline, r.lag = true, current, offline, lag
	}
	return out, lead.Err()
}

// placementOf turns a stream's replica reports, KV and object store
// streams' too, into one square per replica: the leader first, the rest by
// server name, then MISSING squares without a server up to the configured
// replicas. Its rule, in order:
//  1. One LEADER, the first by name of the servers that claim the lead;
//     the other claimants are followers.
//  2. Without a leader: UNSPECIFIED, or DISAGREES when the reporting
//     servers' non-empty config hashes differ.
//  3. Listed by the leader as offline: OFFLINE.
//  4. Reports nothing itself: MISSING.
//  5. Not listed by the leader: DISAGREES.
//  6. Not current, or a lag with current unknown: BEHIND, whatever its
//     config hash, as a follower catching up still has the old one.
//  7. Config hash differs from the leader's (both non-empty): DISAGREES.
//  8. Current: IN_SYNC.
//  9. Otherwise UNSPECIFIED.
func placementOf(reps map[string]*replicaReport, replicas int32) []*v1.ReplicaPlacement {
	leader := ""
	hashes := map[string]bool{}
	names := make([]string, 0, len(reps))
	for name, r := range reps {
		if r.leader && (leader == "" || name < leader) {
			leader = name
		}
		if r.reports && r.hash != "" {
			hashes[r.hash] = true
		}
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if (a == leader) != (b == leader) {
			if a == leader {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	out := make([]*v1.ReplicaPlacement, 0, max(len(names), int(replicas)))
	for _, name := range names {
		st := v1.PlacementState_PLACEMENT_STATE_LEADER
		if name != leader {
			st = replicaState(reps[name], leader != "", reps[leader].hashOr(), len(hashes) > 1)
		}
		out = append(out, &v1.ReplicaPlacement{Server: name, State: st})
	}
	for range int(replicas) - len(names) {
		out = append(out, &v1.ReplicaPlacement{State: v1.PlacementState_PLACEMENT_STATE_MISSING})
	}
	return out
}

// hashOr is the report's config hash; empty for a nil report.
func (r *replicaReport) hashOr() string {
	if r == nil {
		return ""
	}
	return r.hash
}

// replicaState is a follower's state by placementOf's rule.
func replicaState(r *replicaReport, hasLeader bool, leaderHash string, hashesDiffer bool) v1.PlacementState {
	switch {
	case !hasLeader && r.reports && r.hash != "" && hashesDiffer:
		return v1.PlacementState_PLACEMENT_STATE_DISAGREES
	case !hasLeader:
		return v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED
	case r.listed && r.offline.Valid && r.offline.Bool:
		return v1.PlacementState_PLACEMENT_STATE_OFFLINE
	case !r.reports:
		return v1.PlacementState_PLACEMENT_STATE_MISSING
	case !r.listed:
		return v1.PlacementState_PLACEMENT_STATE_DISAGREES
	case (r.current.Valid && !r.current.Bool) || (!r.current.Valid && r.lag.Valid && r.lag.Int64 > 0):
		return v1.PlacementState_PLACEMENT_STATE_BEHIND
	case r.hash != "" && leaderHash != "" && r.hash != leaderHash:
		return v1.PlacementState_PLACEMENT_STATE_DISAGREES
	case r.current.Valid && r.current.Bool:
		return v1.PlacementState_PLACEMENT_STATE_IN_SYNC
	}
	return v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED
}

// lagging reports whether a stream has a replica BEHIND its leader.
func lagging(ps []*v1.ReplicaPlacement) bool {
	return slices.ContainsFunc(ps, func(p *v1.ReplicaPlacement) bool {
		return p.GetState() == v1.PlacementState_PLACEMENT_STATE_BEHIND
	})
}
