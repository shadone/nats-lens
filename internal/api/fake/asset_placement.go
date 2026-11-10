package fake

import (
	"context"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListAssetPlacement answers from the placement matrix of the same range:
// each asset's cells, as servers with their states.
func (p *Provider) ListAssetPlacement(ctx context.Context, req *v1.ListAssetPlacementRequest) (*v1.ListAssetPlacementResponse, error) {
	m, err := p.GetPlacement(ctx, &v1.GetPlacementRequest{Range: req.GetRange()})
	if err != nil {
		return nil, err
	}
	byKey := map[string]*v1.PlacementAsset{}
	for _, s := range m.GetStreams() {
		byKey["s:"+s.GetEntity().GetKey()] = s
		for _, c := range s.GetConsumers() {
			byKey["c:"+c.GetEntity().GetKey()] = c
		}
	}
	res := &v1.ListAssetPlacementResponse{EpochAt: m.GetEpochAt()}
	for _, e := range api.DistinctAssets(req) {
		prefix := "s:"
		if e.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER {
			prefix = "c:"
		}
		out := &v1.AssetPlacement{Entity: e}
		if a := byKey[prefix+e.GetKey()]; a != nil {
			out.Leader = a.GetLeader()
			for _, c := range a.GetCells() {
				out.Replicas = append(out.Replicas, &v1.ReplicaPlacement{Server: c.GetServer(), State: c.GetState()})
			}
		}
		out.Reported = len(out.Replicas) > 0
		res.Assets = append(res.Assets, out)
	}
	return res, nil
}
