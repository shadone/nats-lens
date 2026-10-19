package fake

import (
	"context"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// GetPlacement returns every stream and durable consumer at the range's
// newest epoch: the squares of placementAt, each server's report from the
// views GetStream and GetConsumer list, and the other peers each
// reporting server names.
func (p *Provider) GetPlacement(_ context.Context, req *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error) {
	s := p.scen()
	all := p.findings(s)
	res := &v1.GetPlacementResponse{Servers: s.servers(req.GetRange(), all)}
	at, _, ok := s.epochBefore(req.GetRange())
	if !ok {
		return res, nil
	}
	res.EpochAt = ts(at)
	cur := current(all, req.GetRange())
	for _, sp := range p.sc.streams {
		a := &v1.PlacementAsset{Entity: streamRef(sp.name), Replicas: ptr(int32(len(s.sc.peers(sp)))), ServersDisagree: sp.disagrees != "",
			Messages: ptr(sp.msgs)}
		views := map[string]*v1.PlacementCell{}
		for _, v := range s.streamViews(sp, at) {
			views[v.GetServer()] = &v1.PlacementCell{Created: v.GetCreated(), ConfigHash: v.GetConfigHash(), Lag: v.Lag,
				Messages: v.Messages, FirstSeq: v.FirstSeq, LastSeq: v.LastSeq}
		}
		key := streamRef(sp.name).GetKey()
		leader := s.streamLeaderAt(sp, at)
		s.fillAsset(a, sp, s.placementLed(sp, at, false, leader), views,
			s.leaderSince([]string{key}, s.sc.peers(sp), leader, at, func(t time.Time) string { return s.streamLeaderAt(sp, t) }))
		for _, n := range s.consumersAt(sp, at) {
			c := s.consumerSummary(sp, n, cur, true, at)
			ca := &v1.PlacementAsset{Entity: c.GetEntity(), Replicas: c.Replicas, ServersDisagree: c.GetServersDisagree(), Durable: true}
			cv, _ := s.consumerViews(sp, c, at)
			views := map[string]*v1.PlacementCell{}
			for _, v := range cv {
				views[v.GetServer()] = &v1.PlacementCell{Created: v.GetCreated(), ConfigHash: v.GetConfigHash(), Lag: v.Lag,
					DeliveredStreamSeq: v.DeliveredStreamSeq, AckFloorStreamSeq: v.AckFloorStreamSeq}
			}
			cl := s.consumerLeaderAt(sp, n, at)
			s.fillAsset(ca, sp, s.placementLed(sp, at, true, cl), views, s.leaderSince([]string{key, c.GetEntity().GetKey()}, s.sc.peers(sp), cl, at,
				func(t time.Time) string { return s.consumerLeaderAt(sp, n, t) }))
			a.Consumers = append(a.Consumers, ca)
		}
		res.Streams = append(res.Streams, a)
	}
	return res, nil
}

// fillAsset turns an asset's squares into cells with each reporting
// server's view and the peers it names: every other peer of the stream,
// offline and missing ones included, as a server's replica list names
// them.
func (s scen) fillAsset(a *v1.PlacementAsset, sp streamSpec, squares []*v1.ReplicaPlacement, views map[string]*v1.PlacementCell, since time.Time) {
	peers := slices.Sorted(slices.Values(s.sc.peers(sp)))
	for _, sq := range squares {
		if sq.GetServer() == "" {
			a.Unplaced++
			continue
		}
		c, reports := views[sq.GetServer()]
		if !reports {
			c = &v1.PlacementCell{}
		}
		c.Server, c.State = sq.GetServer(), sq.GetState()
		if c.GetState() == v1.PlacementState_PLACEMENT_STATE_LEADER {
			a.Leader = c.GetServer()
			if !since.IsZero() {
				a.LeaderSince = ts(since)
			}
		}
		if reports {
			for _, n := range peers {
				if n != c.GetServer() {
					c.Peers = append(c.Peers, n)
				}
			}
		}
		a.Cells = append(a.Cells, c)
	}
	api.SetDiffers(a.GetCells(), a.GetLeader())
}
