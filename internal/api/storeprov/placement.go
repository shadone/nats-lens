package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

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
func (p *Provider) streamReplicas(ctx context.Context, epoch int64, account, stream string) (map[string]map[string]*replicaReport, error) {
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
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?)`, epoch, account, account, stream, stream)
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
WHERE rp.epoch = ? AND (? = '' OR rp.account = ?) AND (? = '' OR rp.stream = ?)`, epoch, account, account, stream, stream)
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

// placementOf turns a stream's replica reports into one square per
// replica, by server name. The leader is LEADER. A follower whose config
// hash differs from the leader's DISAGREES. Otherwise the leader's view
// decides: OFFLINE, IN_SYNC when current, BEHIND when not; a replica the
// leader lists that reports nothing itself is MISSING. Without a leader's
// view a replica's state is unset. Configured replicas beyond the named
// ones are MISSING with no server.
func placementOf(reps map[string]*replicaReport, replicas int32) []*v1.ReplicaPlacement {
	leaderHash, hasLeader := "", false
	names := make([]string, 0, len(reps))
	for name, r := range reps {
		names = append(names, name)
		if r.leader && (!hasLeader || r.hash < leaderHash) {
			leaderHash, hasLeader = r.hash, true
		}
	}
	slices.Sort(names)
	out := make([]*v1.ReplicaPlacement, 0, max(len(names), int(replicas)))
	for _, name := range names {
		out = append(out, &v1.ReplicaPlacement{Server: name, State: replicaState(reps[name], hasLeader, leaderHash)})
	}
	for range int(replicas) - len(names) {
		out = append(out, &v1.ReplicaPlacement{State: v1.PlacementState_PLACEMENT_STATE_MISSING})
	}
	return out
}

func replicaState(r *replicaReport, hasLeader bool, leaderHash string) v1.PlacementState {
	switch {
	case r.leader:
		return v1.PlacementState_PLACEMENT_STATE_LEADER
	case r.reports && hasLeader && r.hash != "" && leaderHash != "" && r.hash != leaderHash:
		return v1.PlacementState_PLACEMENT_STATE_DISAGREES
	case !r.listed:
		return v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED
	case r.offline.Bool:
		return v1.PlacementState_PLACEMENT_STATE_OFFLINE
	case !r.reports:
		return v1.PlacementState_PLACEMENT_STATE_MISSING
	case r.current.Valid && r.current.Bool:
		return v1.PlacementState_PLACEMENT_STATE_IN_SYNC
	case r.current.Valid || r.lag.Int64 > 0:
		return v1.PlacementState_PLACEMENT_STATE_BEHIND
	}
	return v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED
}

// lagging reports whether a stream has a replica BEHIND its leader.
func lagging(ps []*v1.ReplicaPlacement) bool {
	return slices.ContainsFunc(ps, func(p *v1.ReplicaPlacement) bool {
		return p.GetState() == v1.PlacementState_PLACEMENT_STATE_BEHIND
	})
}
