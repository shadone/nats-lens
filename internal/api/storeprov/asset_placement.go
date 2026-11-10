package storeprov

import (
	"context"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListAssetPlacement reads the replicas of the streams and consumers asked
// for in the range's newest epoch: one read of the stream replicas and one
// of the consumer replicas, each limited to the keys asked for, placed by
// placementOf as the placement matrix places its cells. Configured
// replicas that no server names are left out, so the configuration is not
// read.
func (p *Provider) ListAssetPlacement(ctx context.Context, req *v1.ListAssetPlacementRequest) (*v1.ListAssetPlacementResponse, error) {
	assets := api.DistinctAssets(req)
	res := &v1.ListAssetPlacementResponse{}
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	reps := map[string]map[string]*replicaReport{}
	if ok {
		res.EpochAt = ts(e.startedAt)
		var streams, consumers []string
		for _, a := range assets {
			if a.GetType() == v1.EntityType_ENTITY_TYPE_STREAM {
				streams = append(streams, a.GetKey())
			} else {
				consumers = append(consumers, a.GetKey())
			}
		}
		if len(streams) > 0 {
			sreps, err := p.streamReplicasScoped(ctx, e.epoch, "", "", "", streams, true)
			if err != nil {
				return nil, err
			}
			for k, v := range sreps {
				reps["s:"+k] = v
			}
		}
		if len(consumers) > 0 {
			creps, err := p.replicaReports(ctx, consumerTables, e.epoch, func(a string) string {
				return fmt.Sprintf("concat_ws('/', %s) IN (SELECT unnest(?::VARCHAR[]))", consumerTables.cols(a))
			}, consumers)
			if err != nil {
				return nil, err
			}
			for k, v := range creps {
				reps["c:"+k] = v
			}
		}
	}
	for _, a := range assets {
		prefix := "s:"
		if a.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER {
			prefix = "c:"
		}
		out := &v1.AssetPlacement{Entity: a}
		for _, sq := range placementOf(reps[prefix+a.GetKey()], 0) {
			if sq.GetState() == v1.PlacementState_PLACEMENT_STATE_LEADER {
				out.Leader = sq.GetServer()
			}
			out.Replicas = append(out.Replicas, sq)
		}
		out.Reported = len(out.Replicas) > 0
		res.Assets = append(res.Assets, out)
	}
	return res, nil
}
