package fake

import (
	"context"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// OnCall reads the same scenario facts as the finding and detail pages.
func (p *Provider) OnCall(ctx context.Context, req *v1.GetOnCallRequest) (*api.OnCallFacts, error) {
	detail, err := p.GetFinding(ctx, &v1.GetFindingRequest{Id: req.GetFindingId()})
	if err != nil {
		return nil, err
	}
	s := p.scen()
	f := &api.OnCallFacts{At: s.now, Finding: detail.Finding, Transitions: detail.Transitions, Firing: p.findings(s), Now: &v1.OnCallNow{}, Assets: map[string]*v1.OnCallAsset{}}
	f.PlaybookURL = demoPlaybooks[f.Finding.GetCheckCode()]
	start := api.OnCallConditionStart(f.Finding).Add(-15 * time.Minute)
	f.Windows = p.windows(s, &v1.TimeRange{Start: ts(start), End: ts(s.now)})
	r := &v1.TimeRange{Start: ts(s.now.Add(-api.ReplayEpochReach * sampleEvery)), End: ts(s.now.Add(time.Microsecond))}
	at, _, ok := s.epochBefore(r)
	if !ok {
		return f, nil
	}
	f.Now.EpochAt = ts(at)
	f.Now.ServersExpected = ptr(int32(len(s.sc.servers)))
	var answering int32
	for _, sp := range s.sc.servers {
		up := !s.downAt(sp, at)
		if up {
			answering++
		}
		f.Assets[api.OnCallEntityKey(serverRef(sp.name))] = &v1.OnCallAsset{Answering: ptr(up)}
	}
	f.Now.ServersAnswering = &answering
	delivered, stored := map[string]float64{}, map[string]float64{}
	for _, sp := range s.sc.streams {
		entity := streamRef(sp.name)
		f.Assets[api.OnCallEntityKey(entity)] = &v1.OnCallAsset{Replicas: ptr(int32(len(s.sc.peers(sp)))), Online: api.OnCallOnline(s.placementAt(sp, at, false)), Leader: s.streamLeaderAt(sp, at)}
		if s.rateWindow(r) {
			stored[entity.Key] = s.sc.storedRate(sp)
		}
		for _, c := range s.consumers(sp, f.Firing, true, at) {
			if !c.Present {
				f.Assets[api.OnCallEntityKey(c.Entity)] = &v1.OnCallAsset{Replicas: c.Replicas}
				continue
			}
			placement := s.placementLed(sp, at, true, c.Leader)
			f.Assets[api.OnCallEntityKey(c.Entity)] = &v1.OnCallAsset{Replicas: c.Replicas, Online: api.OnCallOnline(placement), Leader: c.Leader}
			if s.rateWindow(r) {
				if rate, ok := consumerSample("consumer.delivered_rate", c.Entity, s.clock(at)); ok {
					delivered[c.Entity.Key] = rate
				}
			}
		}
	}
	api.OnCallRates(f, delivered, stored)
	return f, nil
}
