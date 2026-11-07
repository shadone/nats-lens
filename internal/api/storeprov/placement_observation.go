package storeprov

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Proposed, not yet confirmed: a 24-hour, 256-collection lookback and a
// maximum spacing of twice the configured collection interval.
const placementLookback = 24 * time.Hour
const placementCollections = 256

type placementSample struct {
	epoch       int64
	at          time.Time
	reps        map[string]*replicaReport
	cells       map[string]*v1.PlacementCell
	identities  map[string]string
	processes   map[string]string
	peers       map[string][]string
	selectedID  string
	selectedJSZ bool
	unknown     bool
}

// placementObservation adds four bounded, batched reads for one asset. It
// never reads findings or live NATS and does not query once per collection.
func (p *Provider) placementObservation(ctx context.Context, req *v1.GetPlacementRequest, end epochInfo) (*v1.PlacementObservation, error) {
	parts := strings.Split(req.GetSelectedAsset(), "/")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, nil
	}
	t := streamTables
	if len(parts) == 3 {
		t = consumerTables
	}
	start, _ := span(req.GetRange())
	floor := end.startedAt.Add(-placementLookback)
	edge := "lookback_limit"
	if !start.Before(floor) {
		floor = start
		edge = "range_limit"
	}
	rows, err := p.db().QueryContext(ctx, `SELECT epoch, started_at, (SELECT min(started_at) FROM epochs WHERE epoch <= ?) FROM epochs WHERE started_at >= ? AND started_at <= ? AND epoch <= ? ORDER BY epoch DESC LIMIT ?`, end.epoch, floor, end.startedAt, end.epoch, placementCollections)
	if err != nil {
		return nil, err
	}
	var samples []*placementSample
	var oldest time.Time
	byEpoch := map[int64]*placementSample{}
	for rows.Next() {
		s := &placementSample{reps: map[string]*replicaReport{}, cells: map[string]*v1.PlacementCell{}, identities: map[string]string{}, peers: map[string][]string{}}
		if err = rows.Scan(&s.epoch, &s.at, &oldest); err != nil {
			rows.Close()
			return nil, err
		}
		s.at = s.at.UTC()
		samples = append(samples, s)
		byEpoch[s.epoch] = s
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, nil
	}
	if oldest.UTC().After(floor) {
		edge = "history_limit"
	}
	if len(samples) == placementCollections {
		edge = "collection_limit"
	}
	// ASVS 1.2.4: only fixed table/column names enter SQL; all selection values
	// and epoch bounds are parameters, including account isolation.
	where := []string{}
	args := []any{samples[len(samples)-1].epoch, end.epoch}
	for i, k := range t.keys {
		where = append(where, "x."+k+" = ?")
		args = append(args, parts[i])
	}
	predicate := strings.Join(where, " AND ")
	q := fmt.Sprintf(`SELECT x.epoch, s.server_name, x.server_id, coalesce(x.is_leader,false), x.created, coalesce(c.config_hash,''),
 coalesce(ep.state = 'ok',false)
 FROM %s x JOIN servers s USING(server_id)
 LEFT JOIN %s c ON c.server_id=x.server_id AND %s AND x.epoch BETWEEN c.first_epoch AND c.last_epoch
 LEFT JOIN epoch_endpoints ep ON ep.epoch=x.epoch AND ep.server_id=x.server_id AND ep.endpoint='JSZ'
 WHERE x.epoch BETWEEN ? AND ? AND %s`, t.stats, t.config, t.on("c", "x"), predicate)
	rows, err = p.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var epoch int64
		var name, id, hash string
		var leader, ok bool
		var created sql.NullTime
		if err = rows.Scan(&epoch, &name, &id, &leader, &created, &hash, &ok); err != nil {
			break
		}
		s := byEpoch[epoch]
		if s == nil {
			continue
		}
		if _, exists := s.reps[name]; exists {
			s.unknown = true
		}
		s.reps[name] = &replicaReport{reports: true, leader: leader, hash: hash, byLeader: map[string]claimantView{}}
		s.cells[name] = &v1.PlacementCell{Created: createdOrNil(ts(ntime(created))), ConfigHash: hash}
		// Missing creation or endpoint evidence cannot establish identity continuity.
		if !created.Valid || created.Time.Unix() <= 0 || !ok {
			s.unknown = true
		}
		s.identities[name] = id + "/" + ntime(created).Format(time.RFC3339Nano)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	q = fmt.Sprintf(`SELECT x.epoch, s.server_name, x.peer_name, x.current, x.offline, x.lag
 FROM %s x JOIN servers s USING(server_id) WHERE x.epoch BETWEEN ? AND ? AND %s`, t.replicas, predicate)
	rows, err = p.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var epoch int64
		var name, peer string
		var v claimantView
		if err = rows.Scan(&epoch, &name, &peer, &v.current, &v.offline, &v.lag); err != nil {
			break
		}
		s := byEpoch[epoch]
		if s == nil {
			continue
		}
		s.peers[name] = append(s.peers[name], peer)
		if r := s.reps[name]; r != nil && r.leader {
			if s.reps[peer] == nil {
				s.reps[peer] = &replicaReport{byLeader: map[string]claimantView{}}
			}
			s.reps[peer].byLeader[name] = v
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Resolve every name used by this sample, including peers with no asset
	// report. A name is not a process ID: simultaneous observations of two
	// same-named processes make the sample unknown, regardless of which JSZ
	// endpoint succeeded. The sole observed process is authoritative, even if
	// an older identity returns. With no observation, a single endpoint target
	// (including a failed reply) identifies the attempted process. Historical
	// fallback is uncertain when more than one prior identity could apply.
	names := map[string]bool{req.GetSelectedServer(): true}
	for _, s := range samples {
		for name := range s.reps {
			names[name] = true
		}
		for _, peers := range s.peers {
			for _, name := range peers {
				names[name] = true
			}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	slices.Sort(ordered)
	low, high := samples[len(samples)-1].epoch, end.epoch
	identityArgs := []any{low, high, low, high, low, high}
	for _, name := range ordered {
		identityArgs = append(identityArgs, name)
	}
	q = `WITH observed AS (
 SELECT epoch, server_id FROM server_stats WHERE epoch BETWEEN ? AND ?
 UNION
 SELECT epoch, server_id FROM epoch_endpoints WHERE epoch BETWEEN ? AND ? AND state='ok'
), candidates AS (
 SELECT e.epoch, s.server_name, s.server_id, s.first_epoch,
 coalesce(ep.state='ok',false) AS jsz_ok,
 o.server_id IS NOT NULL AS is_observed,
 ep.server_id IS NOT NULL AS is_target,
 count(o.server_id) OVER (PARTITION BY e.epoch,s.server_name) AS observed_processes,
 count(ep.server_id) OVER (PARTITION BY e.epoch,s.server_name) AS targets,
 count(*) OVER (PARTITION BY e.epoch,s.server_name) AS historical_processes,
 count(*) OVER (PARTITION BY e.epoch,s.server_name,s.first_epoch) AS same_first_processes
 FROM epochs e JOIN servers s ON s.first_epoch<=e.epoch
 LEFT JOIN observed o ON o.epoch=e.epoch AND o.server_id=s.server_id
 LEFT JOIN epoch_endpoints ep ON ep.epoch=e.epoch AND ep.server_id=s.server_id AND ep.endpoint='JSZ'
 WHERE e.epoch BETWEEN ? AND ? AND s.server_name IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ordered)), ",") + `)
)
SELECT epoch, server_name, server_id, jsz_ok, observed_processes,
 targets, historical_processes, first_epoch, is_target, same_first_processes FROM candidates
QUALIFY row_number() OVER(PARTITION BY epoch,server_name ORDER BY is_observed DESC,is_target DESC,first_epoch DESC,server_id)=1`
	// Both observed sources and the result stay inside the same collection
	// bounds. This remains the fourth read, not one query per name or epoch.
	rows, err = p.db().QueryContext(ctx, q, identityArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var epoch, observed, targets, historical, first, sameFirst int64
		var name, id string
		var ok, target bool
		if err = rows.Scan(&epoch, &name, &id, &ok, &observed, &targets, &historical, &first, &target, &sameFirst); err != nil {
			break
		}
		if s := byEpoch[epoch]; s != nil {
			relevant := name == req.GetSelectedServer() || s.reps[name] != nil
			for _, peers := range s.peers {
				relevant = relevant || slices.Contains(peers, name)
			}
			if !relevant {
				continue
			}
			if s.processes == nil {
				s.processes = map[string]string{}
			}
			s.processes[name] = id
			// Multiple current targets can disagree even if only one replied.
			// Without current evidence, first_epoch alone cannot resolve an
			// old process returning after a replacement. A newly first-seen
			// process still establishes a clean handover in that collection.
			conflictingTarget := observed == 1 && targets > 0 && !target
			ambiguousHistory := observed == 0 && targets == 0 && historical > 1 && (first != epoch || sameFirst > 1)
			if observed > 1 || targets > 1 || conflictingTarget || ambiguousHistory {
				s.unknown = true
			}
			if name == req.GetSelectedServer() {
				s.selectedID = id
				s.selectedJSZ = ok
			}
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, s := range samples {
		if s.processes[req.GetSelectedServer()] == "" {
			s.unknown = true
		}
		for _, peers := range s.peers {
			for _, name := range peers {
				if s.processes[name] == "" {
					s.unknown = true
				}
			}
		}
	}
	return observePlacement(samples, req.GetSelectedAsset(), req.GetSelectedServer(), edge, p.Interval), nil
}

// classifySample uses precisely the current matrix's classifier and settings
// comparison. The signature also preserves the disagreement's actual values,
// process/incarnation identities and every reporter's membership list.
func classifySample(s *placementSample, server string) (*v1.PlacementCell, string) {
	a := &v1.PlacementAsset{}
	fillAsset(a, placementOf(s.reps, 0), s.cells, s.reps, extras{peers: map[string]map[string][]string{"": s.peers}}, "")
	var selected *v1.PlacementCell
	hashes := map[string]string{}
	disagrees := false
	for _, c := range a.Cells {
		if c.Server == server {
			selected = c
		}
		if len(c.Differs) > 0 || c.State == v1.PlacementState_PLACEMENT_STATE_DISAGREES {
			disagrees = true
		}
	}
	if disagrees {
		for _, c := range a.Cells {
			hashes[c.Server] = c.ConfigHash
		}
	}
	for _, peers := range s.peers {
		slices.Sort(peers)
	}
	b, _ := json.Marshal([]any{s.identities, s.processes, s.peers, s.selectedID, a.Leader, hashes})
	return selected, string(b)
}

func observePlacement(samples []*placementSample, asset, server, edge string, interval time.Duration) *v1.PlacementObservation {
	if len(samples) == 0 {
		return nil
	}
	c, identity := classifySample(samples[0], server)
	if c == nil {
		return nil
	}
	switch c.State {
	case v1.PlacementState_PLACEMENT_STATE_BEHIND, v1.PlacementState_PLACEMENT_STATE_OFFLINE, v1.PlacementState_PLACEMENT_STATE_MISSING, v1.PlacementState_PLACEMENT_STATE_DISAGREES:
	case v1.PlacementState_PLACEMENT_STATE_IN_SYNC:
		if len(c.Differs) == 0 {
			return nil
		}
	default:
		return nil
	}
	out := &v1.PlacementObservation{Asset: asset, Server: server, AsOf: ts(samples[0].at), Since: ts(samples[0].at), Samples: 1, State: c.State, Differs: c.Differs, Boundary: edge, Truncated: true}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if samples[0].unknown || (c.State == v1.PlacementState_PLACEMENT_STATE_MISSING && !samples[0].selectedJSZ) {
		out.Boundary = "evidence_unknown"
		out.Truncated = false
		return out
	}
	for i := 1; i < len(samples); i++ {
		s := samples[i]
		prev := samples[i-1]
		boundary := ""
		old, sig := classifySample(s, server)
		switch {
		case prev.epoch != s.epoch+1 || !prev.at.After(s.at) || prev.at.Sub(s.at) > 2*interval:
			boundary = "collection_gap"
		case s.unknown || old == nil || old.State == v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED || (old.State == v1.PlacementState_PLACEMENT_STATE_MISSING && !s.selectedJSZ):
			boundary = "evidence_unknown"
		case old.State != c.State || !slices.Equal(old.Differs, c.Differs):
			boundary = "state_changed"
		case sig != identity:
			boundary = "identity_changed"
		}
		if boundary != "" {
			out.Boundary = boundary
			out.Truncated = false
			break
		}
		out.Since = ts(s.at)
		out.Samples++
	}
	return out
}
