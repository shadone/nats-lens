package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// OnCall reuses finding conversion and batch placement/rate readers. The
// query count is independent of the number of affected assets.
func (p *Provider) OnCall(ctx context.Context, req *v1.GetOnCallRequest) (*api.OnCallFacts, error) {
	detail, err := p.GetFinding(ctx, &v1.GetFindingRequest{Id: req.GetFindingId()})
	if err != nil {
		return nil, err
	}
	f := &api.OnCallFacts{At: p.Now(), Finding: detail.Finding, Transitions: detail.Transitions, Now: &v1.OnCallNow{}, Assets: map[string]*v1.OnCallAsset{}}
	stored, err := p.St.Findings(ctx, f.At, store.FindingQuery{CheckCode: f.Finding.GetCheckCode(), FiringOnly: true})
	if err != nil {
		return nil, err
	}
	if f.Firing, err = p.findingProtos(ctx, stored); err != nil {
		return nil, err
	}
	if p.Checks != nil {
		if c, ok := p.Checks.Lookup(f.Finding.GetCheckCode()); ok {
			f.PlaybookURL = c.Playbook
		}
	}
	// Context may precede the bounded story. Read windows separately so
	// missing Replay history does not discard the context or present facts.
	start := api.OnCallConditionStart(f.Finding).Add(-15 * time.Minute)
	if p.Kubernetes {
		f.Windows, err = p.k8sWindows(ctx, &v1.TimeRange{Start: ts(start), End: ts(f.At)})
		if err != nil {
			return nil, err
		}
	}
	// ASVS 1.2.4: times remain bound parameters compared directly with
	// TIMESTAMP columns, including on hosts outside UTC.
	var e epochInfo
	var expected, observed int32
	err = p.db().QueryRowContext(ctx, `SELECT epoch, started_at, expected_servers, observed_servers FROM epochs
 WHERE started_at <= ? AND started_at >= ? ORDER BY epoch DESC LIMIT 1`, f.At, f.At.Add(-api.ReplayEpochReach*max(p.Interval, time.Second))).Scan(&e.epoch, &e.startedAt, &expected, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	e.startedAt = e.startedAt.UTC()
	f.Now.EpochAt = ts(e.startedAt)
	f.Now.ServersExpected = &expected
	f.Now.ServersAnswering = &observed
	affected := api.OnCallAffected(f.Finding, f.Firing)
	if len(affected) == 0 {
		affected = []*v1.Finding{f.Finding}
	}
	typ := f.Finding.GetEntity().GetType()
	if typ == v1.EntityType_ENTITY_TYPE_SERVER {
		// Answering is known from the epoch, even for a missing server.
		rows, err := p.db().QueryContext(ctx, `SELECT DISTINCT s.server_name FROM epoch_endpoints x JOIN servers s USING(server_id) WHERE x.epoch = ? AND x.state = 'ok'`, e.epoch)
		if err != nil {
			return nil, err
		}
		answering := map[string]bool{}
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				rows.Close()
				return nil, err
			}
			answering[name] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, v := range affected[:min(10, len(affected))] {
			f.Assets[api.OnCallEntityKey(v.Entity)] = &v1.OnCallAsset{Answering: ptr(answering[v.Entity.GetKey()])}
		}
		return f, nil
	}
	if typ != v1.EntityType_ENTITY_TYPE_STREAM && typ != v1.EntityType_ENTITY_TYPE_CONSUMER {
		return f, nil
	}
	if typ == v1.EntityType_ENTITY_TYPE_STREAM {
		streams, _, err := p.streamBase(ctx, e, "", "")
		if err != nil {
			return nil, err
		}
		for _, s := range streams {
			f.Assets[api.OnCallEntityKey(s.Entity)] = &v1.OnCallAsset{Replicas: s.Replicas, Online: api.OnCallOnline(s.Placement), Leader: s.Leader}
		}
	}
	if typ == v1.EntityType_ENTITY_TYPE_CONSUMER {
		cons, err := p.consumerRows(ctx, e, "", "", "")
		if err != nil {
			return nil, err
		}
		inv, _, err := p.inventory(ctx)
		if err != nil {
			return nil, err
		}
		cons = append(cons, missingConsumers(inv, cons, "", "", "")...)
		reps, err := p.consumerReplicas(ctx, e.epoch)
		if err != nil {
			return nil, err
		}
		for _, c := range cons {
			f.Assets[api.OnCallEntityKey(c.Entity)] = &v1.OnCallAsset{Replicas: c.Replicas, Online: api.OnCallOnline(placementOf(reps[c.Entity.GetKey()], c.GetReplicas())), Leader: c.Leader}
		}
	}
	// Rates require leader reports in this collection, independently of the story.
	delivered, err := p.onCallRates(ctx, e, true)
	if err != nil {
		return nil, err
	}
	rates, err := p.onCallRates(ctx, e, false)
	if err != nil {
		return nil, err
	}
	api.OnCallRates(f, delivered, rates)
	return f, nil
}
