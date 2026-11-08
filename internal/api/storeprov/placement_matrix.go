package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// GetPlacement reads every stream and durable consumer of the range's
// newest epoch with one cell per replica: the states of the summaries'
// placement squares (placementOf), each server's own report from the
// views GetStream and GetConsumer list, and the peers each server names.
func (p *Provider) GetPlacement(ctx context.Context, req *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error) {
	servers, err := p.listServers(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	res := &v1.GetPlacementResponse{Servers: servers.GetServers()}
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return res, err
	}
	res.EpochAt = ts(e.startedAt)
	streams, _, err := p.streamBase(ctx, e, "", "")
	if err != nil {
		return nil, err
	}
	sviews, err := p.streamViewsBy(ctx, e.epoch, "", "")
	if err != nil {
		return nil, err
	}
	sreps, err := p.streamReplicasAll(ctx, e.epoch, "", "", "")
	if err != nil {
		return nil, err
	}
	sx, err := p.placementExtras(ctx, streamTables, e.epoch)
	if err != nil {
		return nil, err
	}
	byStream := map[string]*v1.PlacementAsset{}
	for _, s := range streams {
		key := s.GetEntity().GetKey()
		a := &v1.PlacementAsset{Entity: s.GetEntity(), Replicas: s.Replicas, ServersDisagree: s.GetServersDisagree(), Messages: s.Messages}
		views := map[string]*v1.PlacementCell{}
		for _, v := range sviews[key] {
			views[v.GetServer()] = &v1.PlacementCell{Created: createdOrNil(v.GetCreated()), ConfigHash: v.GetConfigHash(),
				Messages: v.Messages, FirstSeq: v.FirstSeq, LastSeq: v.LastSeq}
		}
		fillAsset(a, s.GetPlacement(), views, sreps[key], sx, key)
		byStream[key] = a
		res.Streams = append(res.Streams, a)
	}
	cons, err := p.consumerRows(ctx, e, "", "", "")
	if err != nil {
		return nil, err
	}
	creps, err := p.consumerReplicas(ctx, e.epoch)
	if err != nil {
		return nil, err
	}
	cviews, err := p.consumerViewsBy(ctx, e.epoch, "", "", "")
	if err != nil {
		return nil, err
	}
	cx, err := p.placementExtras(ctx, consumerTables, e.epoch)
	if err != nil {
		return nil, err
	}
	for _, c := range cons {
		parent := byStream[c.GetEntity().GetAccount()+"/"+c.GetEntity().GetStream()]
		if !c.GetDurable() || parent == nil {
			continue
		}
		key := c.GetEntity().GetKey()
		a := &v1.PlacementAsset{Entity: c.GetEntity(), Replicas: c.Replicas, ServersDisagree: c.GetServersDisagree(), Durable: true}
		views := map[string]*v1.PlacementCell{}
		for _, v := range cviews[key] {
			views[v.GetServer()] = &v1.PlacementCell{Created: createdOrNil(v.GetCreated()), ConfigHash: v.GetConfigHash(),
				DeliveredStreamSeq: v.DeliveredStreamSeq, AckFloorStreamSeq: v.AckFloorStreamSeq}
		}
		fillAsset(a, placementOf(creps[key], c.GetReplicas()), views, creps[key], cx, key)
		parent.Consumers = append(parent.Consumers, a)
	}
	if cell := selectedMatrixCell(res, req.GetSelectedAsset(), req.GetSelectedServer()); cell != nil {
		res.Observation, err = p.placementObservation(ctx, req, e)
		if err != nil {
			return nil, err
		}
		// An ambiguous historical reconstruction must never describe another
		// state than the current matrix (for example duplicate process names).
		if o := res.Observation; o != nil && (o.State != cell.State || !slices.Equal(o.Differs, cell.Differs)) {
			res.Observation = nil
		}
	}
	return res, nil
}

func selectedMatrixCell(res *v1.GetPlacementResponse, key, server string) *v1.PlacementCell {
	if key == "" || server == "" {
		return nil
	}
	find := func(a *v1.PlacementAsset) *v1.PlacementCell {
		if key == "" || a.GetEntity().GetKey() != key {
			return nil
		}
		for _, c := range a.GetCells() {
			if c.Server == server {
				return c
			}
		}
		return nil
	}
	for _, s := range res.GetStreams() {
		if c := find(s); c != nil {
			return c
		}
		for _, a := range s.GetConsumers() {
			if c := find(a); c != nil {
				return c
			}
		}
	}
	return nil
}

// createdOrNil is a view's creation time, nil when it reports none.
func createdOrNil(t *timestamppb.Timestamp) *timestamppb.Timestamp {
	if t == nil || t.AsTime().Unix() <= 0 {
		return nil
	}
	return t
}

// fillAsset turns an asset's placement squares into cells, with each
// server's report from views, the lag the chosen leader reports in reps
// (the list placementOf reads, never another claimant's), the peers it
// names and the chosen leader's tenure.
func fillAsset(a *v1.PlacementAsset, squares []*v1.ReplicaPlacement, views map[string]*v1.PlacementCell, reps map[string]*replicaReport,
	x extras, key string) {
	for _, sq := range squares {
		if sq.GetServer() == "" {
			a.Unplaced++
			continue
		}
		c := views[sq.GetServer()]
		if c == nil {
			c = &v1.PlacementCell{}
		}
		c.Server, c.State, c.Peers = sq.GetServer(), sq.GetState(), x.peers[key][sq.GetServer()]
		if c.GetState() == v1.PlacementState_PLACEMENT_STATE_LEADER {
			a.Leader = c.GetServer()
		}
		a.Cells = append(a.Cells, c)
	}
	for _, c := range a.GetCells() {
		c.Lag = nil
		if r := reps[c.GetServer()]; r != nil && a.GetLeader() != "" && c.GetServer() != a.GetLeader() {
			if v, ok := r.byLeader[a.GetLeader()]; ok && v.lag.Valid {
				c.Lag = &v.lag.Int64
			}
		}
	}
	if t, ok := x.leaderSince[key][a.GetLeader()]; ok && a.GetLeader() != "" {
		a.LeaderSince = ts(t)
	}
	api.SetDiffers(a.GetCells(), a.GetLeader())
}

// extras are what the matrix reads beside the views, keyed by asset key
// and server name: the peers each server names, and when each server
// that claims the lead took it.
type extras struct {
	peers       map[string]map[string][]string
	leaderSince map[string]map[string]time.Time
}

// placementExtras reads the extras of every asset of one kind in the
// epoch.
func (p *Provider) placementExtras(ctx context.Context, t assetTables, epoch int64) (extras, error) {
	x := extras{peers: map[string]map[string][]string{}, leaderSince: map[string]map[string]time.Time{}}
	rows, err := p.db().QueryContext(ctx, fmt.Sprintf(`
SELECT %s, s.server_name, list(DISTINCT rp.peer_name ORDER BY rp.peer_name)
FROM %s rp JOIN servers s USING (server_id)
WHERE rp.epoch = ? GROUP BY ALL`, t.cols("rp"), t.replicas), epoch)
	if err != nil {
		return x, fmt.Errorf("%s peers: %w", t.replicas, err)
	}
	defer rows.Close()
	for rows.Next() {
		var server string
		var peers []any
		key, err := scanKey(rows, len(t.keys), &server, &peers)
		if err != nil {
			return x, err
		}
		if x.peers[key] == nil {
			x.peers[key] = map[string][]string{}
		}
		x.peers[key][server] = strs(peers)
	}
	if err := rows.Err(); err != nil {
		return x, err
	}
	since, err := p.db().QueryContext(ctx, fmt.Sprintf(`
SELECT %s, s.server_name, max(x.leader_since) FROM %s x JOIN servers s USING (server_id)
WHERE x.epoch = ? AND x.is_leader AND x.leader_since IS NOT NULL GROUP BY ALL`,
		t.cols("x"), t.stats), epoch)
	if err != nil {
		return x, fmt.Errorf("%s leader since: %w", t.stats, err)
	}
	defer since.Close()
	for since.Next() {
		var server string
		var at sql.NullTime
		key, err := scanKey(since, len(t.keys), &server, &at)
		if err != nil {
			return x, err
		}
		if at.Valid {
			if x.leaderSince[key] == nil {
				x.leaderSince[key] = map[string]time.Time{}
			}
			x.leaderSince[key][server] = at.Time.UTC()
		}
	}
	return x, since.Err()
}
