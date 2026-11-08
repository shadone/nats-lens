package storeprov

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replicaReport is what epoch's rows say of one replica of a stream: the
// server's own report, and the leader's view of it. The store reader
// keeps every claimant's view in byLeader; placementOf reads the chosen
// leader's into listed, current, offline and lag.
type replicaReport struct {
	reports, leader bool
	hash            string
	listed          bool // the leader lists the replica
	current         sql.NullBool
	offline         sql.NullBool
	lag             sql.NullInt64
	byLeader        map[string]claimantView
}

// claimantView is one leader claimant's entry for a replica.
type claimantView struct {
	current, offline sql.NullBool
	lag              sql.NullInt64
}

// withLeader is the report as the chosen leader sees it, when the report
// holds each claimant's view; a copy, the input is left alone.
func (r *replicaReport) withLeader(leader string) *replicaReport {
	if r.byLeader == nil {
		return r
	}
	c := *r
	v, ok := r.byLeader[leader]
	c.listed, c.current, c.offline, c.lag = ok, v.current, v.offline, v.lag
	return &c
}

// assetTables names the tables of one kind of replicated asset and the
// columns that key it; the names are constants, never input.
type assetTables struct {
	stats, config, replicas string
	keys                    []string
}

var (
	streamTables   = assetTables{"stream_stats", "stream_config", "stream_replicas", []string{"account", "stream"}}
	consumerTables = assetTables{"consumer_stats", "consumer_config", "consumer_replicas", []string{"account", "stream", "consumer"}}
)

// cols is the key columns of alias a, comma separated.
func (t assetTables) cols(a string) string {
	out := make([]string, len(t.keys))
	for i, k := range t.keys {
		out[i] = a + "." + k
	}
	return strings.Join(out, ", ")
}

// on is a join condition matching the key columns of aliases a and b.
func (t assetTables) on(a, b string) string {
	out := make([]string, len(t.keys))
	for i, k := range t.keys {
		out[i] = fmt.Sprintf("%s.%s = %s.%s", a, k, b, k)
	}
	return strings.Join(out, " AND ")
}

// scanKey scans the key columns and the rest of a row; the key is the
// columns joined by "/", as an entity's key.
func scanKey(rows *sql.Rows, n int, rest ...any) (string, error) {
	keys := make([]string, n)
	dest := make([]any, 0, n+len(rest))
	for i := range keys {
		dest = append(dest, &keys[i])
	}
	if err := rows.Scan(append(dest, rest...)...); err != nil {
		return "", err
	}
	return strings.Join(keys, "/"), nil
}

// streamReplicas reads, for every stream of the epoch (optionally one
// account or stream), each replica's report keyed by server name.
// With no explicit stream or bucket prefix this reads ordinary streams only.
// Legacy callers that require hidden buckets use streamReplicasAll explicitly.
func (p *Provider) streamReplicas(ctx context.Context, epoch int64, account, stream, prefix string) (map[string]map[string]*replicaReport, error) {
	return p.streamReplicasScoped(ctx, epoch, account, stream, prefix, nil, prefix != "" || stream != "")
}

// streamReplicasAll preserves the all-asset scope of legacy placement and tile callers.
func (p *Provider) streamReplicasAll(ctx context.Context, epoch int64, account, stream, prefix string) (map[string]map[string]*replicaReport, error) {
	return p.streamReplicasScoped(ctx, epoch, account, stream, prefix, nil, true)
}

func (p *Provider) streamReplicasScoped(ctx context.Context, epoch int64, account, stream, prefix string, keys []string, includeKV bool) (map[string]map[string]*replicaReport, error) {
	return p.replicaReports(ctx, streamTables, epoch, func(a string) string {
		return fmt.Sprintf("(? = '' OR %[1]s.account = ?) AND (? = '' OR %[1]s.stream = ?) AND starts_with(%[1]s.stream, ?) AND (? OR (NOT starts_with(%[1]s.stream, 'KV_') AND NOT starts_with(%[1]s.stream, 'OBJ_'))) AND (? OR %[1]s.account || '/' || %[1]s.stream IN (SELECT unnest(?::VARCHAR[])))", a)
	}, account, account, stream, stream, prefix, includeKV, keys == nil, keys)
}

// consumerReplicas reads each replica's report of every consumer of the
// epoch, keyed by consumer key and server name.
func (p *Provider) consumerReplicas(ctx context.Context, epoch int64) (map[string]map[string]*replicaReport, error) {
	return p.replicaReports(ctx, consumerTables, epoch, func(string) string { return "true" })
}

// replicaReports reads each replica's report of every asset of the epoch
// that passes where, a condition on the alias it is given with args,
// keyed by asset key and server name.
func (p *Provider) replicaReports(ctx context.Context, t assetTables, epoch int64, where func(alias string) string, args ...any) (map[string]map[string]*replicaReport, error) {
	out := map[string]map[string]*replicaReport{}
	get := func(key, server string) (*replicaReport, error) {
		if out[key] == nil {
			if len(out) >= api.MaxListRankingRows {
				return nil, metadataOverflow()
			}
			out[key] = map[string]*replicaReport{}
		}
		if out[key][server] == nil {
			out[key][server] = &replicaReport{byLeader: map[string]claimantView{}}
		}
		return out[key][server], nil
	}
	budget := newMetadataBudget()
	keySQL := func(a string) string { return "concat_ws('/', " + t.cols(a) + ")" }
	q := fmt.Sprintf(`SELECT %s AS key, s.server_name AS server, coalesce(x.is_leader, false) AS leader, coalesce(sc.config_hash, '') AS hash
FROM %s x JOIN servers s USING (server_id)
LEFT JOIN %s sc ON sc.server_id = x.server_id AND %s AND x.epoch BETWEEN sc.first_epoch AND sc.last_epoch
WHERE x.epoch = ? AND `, keySQL("x"), t.stats, t.config, t.on("sc", "x")) + where("x")
	err := p.readMetadata(ctx, q, append([]any{epoch}, args...), budget, func(raw []byte) error {
		var v struct {
			Key, Server, Hash string
			Leader            bool
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		r, err := get(v.Key, v.Server)
		if err != nil {
			return err
		}
		r.reports, r.leader, r.hash = true, r.leader || v.Leader, v.Hash
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Keep every claimant's view; never fabricate healthy placement on overflow.
	q = fmt.Sprintf(`SELECT %s AS key, ls.server_name AS leader, rp.peer_name AS peer, rp.current, rp.offline, rp.lag FROM %s rp
JOIN %s l ON l.epoch = rp.epoch AND l.server_id = rp.server_id AND %s AND l.is_leader
JOIN servers ls ON ls.server_id = rp.server_id
WHERE rp.epoch = ? AND `, keySQL("rp"), t.replicas, t.stats, t.on("l", "rp")) + where("rp")
	err = p.readMetadata(ctx, q, append([]any{epoch}, args...), budget, func(raw []byte) error {
		var v struct {
			Key, Leader, Peer string
			Current, Offline  *bool
			Lag               *int64
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		r, err := get(v.Key, v.Peer)
		if err != nil {
			return err
		}
		view := claimantView{}
		if v.Current != nil {
			view.current = sql.NullBool{Bool: *v.Current, Valid: true}
		}
		if v.Offline != nil {
			view.offline = sql.NullBool{Bool: *v.Offline, Valid: true}
		}
		if v.Lag != nil {
			view.lag = sql.NullInt64{Int64: *v.Lag, Valid: true}
		}
		r.byLeader[v.Leader] = view
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
			st = replicaState(reps[name].withLeader(leader), leader != "", reps[leader].hashOr(), len(hashes) > 1)
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
