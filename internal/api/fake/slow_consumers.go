package fake

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// slowReportAt maps scenario detections to the next synthetic report endpoint.
// Charts, summaries and investigation all select this endpoint in [start,end).
func slowReportAt(at time.Time) time.Time {
	return api.BucketStart(at, sampleEvery).Add(sampleEvery)
}

func (p *Provider) GetSlowConsumerInvestigation(ctx context.Context, req *v1.GetSlowConsumerInvestigationRequest) (*v1.GetSlowConsumerInvestigationResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := p.scen()
	start, end := req.Range.Start.AsTime(), req.Range.End.AsTime()
	out := &v1.GetSlowConsumerInvestigationResponse{Range: req.Range, Provenance: "Demo collection counters, recorded scenario events and findings; synthetic evidence, not a live NATS deployment.", AttributionLimit: api.SlowConsumerAttributionLimit, EventsStart: ts(s.dataStart()), EventsCaptured: true, CaptureGaps: start.Before(s.dataStart()) || end.After(s.dataEnd().Add(time.Second))}
	capture := &api.AccountsFacts{EpochAt: end, WindowStart: start}
	captureGaps(capture, p.events(s))
	out.CaptureGaps = out.CaptureGaps || capture.CaptureGaps
	for at := latest(start, s.dataStart()); at.Before(earliest(end, s.dataEnd())); at = at.Add(sampleEvery) {
		if s.anyDown(at) {
			out.IncompleteCollections++
		}
	}
	// The demo derives counter reports from its scenario, on the same sample
	// grid as its series. It never exposes an event time as a counter time.
	for _, sp := range p.sc.servers {
		if req.Server != "" && req.Server != sp.name {
			continue
		}
		counts := map[time.Time]int64{}
		for _, fixed := range s.sc.slowTimes(sp) {
			at := fixed.Add(s.shift)
			stop := slowReportAt(at)
			if !stop.Before(start) && stop.Before(earliest(end, s.dataEnd())) {
				counts[stop]++
			}
		}
		for stop, n := range counts {
			id := sp.id
			if stop.Before(s.started(sp)) {
				id = sp.oldID
			}
			if req.ObservationServerId != "" && (req.ObservationServerId != id || req.ObservationEpoch != s.epochNumber(stop)) {
				continue
			}
			out.Observations = append(out.Observations, &v1.SlowConsumerObservation{Server: sp.name, ServerId: id, Epoch: s.epochNumber(stop), IntervalStart: ts(stop.Add(-sampleEvery)), IntervalEnd: ts(stop), Increase: ptr(n)})
			out.Detections += n
		}
	}
	slices.SortFunc(out.Observations, func(a, b *v1.SlowConsumerObservation) int {
		if c := b.IntervalEnd.AsTime().Compare(a.IntervalEnd.AsTime()); c != 0 {
			return c
		}
		return strings.Compare(a.ServerId, b.ServerId)
	})
	for _, e := range p.events(s) {
		if !inRange(e.Time.AsTime(), req.Range) || (req.Server != "" && e.Server != req.Server) {
			continue
		}
		if e.Kind == "client.disconnect" && strings.Contains(strings.ToLower(e.Reason), "slow consumer") && (req.Account == "" || req.Account == e.Account) {
			var raw struct {
				Server struct {
					ID string `json:"id"`
				} `json:"server"`
				Client struct {
					ID json.Number `json:"id"`
				} `json:"client"`
			}
			_ = json.Unmarshal([]byte(e.Raw), &raw)
			e.Raw = ""
			out.Events = append(out.Events, &v1.SlowConsumerEvent{Event: e, ServerId: raw.Server.ID, ConnectionId: string(raw.Client.ID)})
			out.RecordedDisconnects++
		}
	}
	for _, f := range p.findings(s) {
		if f.CheckCode != "server-009" && f.CheckCode != "client-003" {
			continue
		}
		supported := f.CheckCode == "server-009" && (req.Server == "" || f.Entity.Key == req.Server)
		for _, e := range out.Events {
			if f.CheckCode == "client-003" && e.Event.ClientName == f.Entity.Key {
				supported = true
			}
		}
		if !supported {
			continue
		}
		for _, t := range s.transitions(f) {
			if inRange(t.Time.AsTime(), req.Range) {
				out.Findings = append(out.Findings, &v1.SlowConsumerFinding{FindingId: f.Id, CheckCode: f.CheckCode, Entity: f.Entity, Time: t.Time, Detail: t.Detail})
			}
		}
	}
	limit := int(req.Limit)
	if len(out.Observations) > limit {
		out.ObservationsTruncated = true
		out.Observations = out.Observations[:limit]
	}
	if len(out.Events) > limit {
		out.EventsTruncated = true
		out.Events = out.Events[:limit]
	}
	if len(out.Findings) > limit {
		out.FindingsTruncated = true
		out.Findings = out.Findings[:limit]
	}
	return out, nil
}
