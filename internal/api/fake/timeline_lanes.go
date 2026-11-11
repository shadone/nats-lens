package fake

import (
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// subjectLanes fills the server, leader and client lanes of res, the
// captured flags and the capture gaps from the captured events and the
// scenario's restarts, by the store's rules; for the whole system only.
func (p *Provider) subjectLanes(s scen, events []*v1.Event, req *v1.GetStatusTimelineRequest, res *v1.GetStatusTimelineResponse) {
	if !api.SubjectLanesFor(req.GetEntity()) {
		return
	}
	r := req.GetRange()
	start, end := r.GetStart().AsTime(), r.GetEnd().AsTime()
	var gaps []api.ReplayGap
	var conns []*v1.Event
	bucket := req.GetBucket().AsDuration()
	var servers, leaders []*v1.LaneEvent
	var counts []api.ClientEventCount
	// The events are newest first; the connection gaps need them oldest
	// first.
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		at := e.GetTime().AsTime()
		if e.GetKind() == "collector.gap" && !at.After(end) && !e.GetReceivedAt().AsTime().Before(start) {
			gaps = append(gaps, api.ReplayGap{Kind: api.GapCollector, Start: at, End: e.GetReceivedAt().AsTime(), Event: e})
		}
		if e.GetSource() == v1.EventSource_EVENT_SOURCE_COLLECTOR && !at.After(end) {
			conns = append(conns, e)
		}
		if e.GetSource() == srcNATS && at.Before(end) && !at.Before(api.CaptureSince(start, end)) {
			if api.SystemEventKind(e.GetKind()) {
				res.SystemEventsCaptured = true
			} else {
				res.JetstreamEventsCaptured = true
			}
		}
		if e.GetSource() != srcNATS || !inRange(at, r) {
			continue
		}
		switch k := e.GetKind(); {
		case slices.Contains(api.ServerLaneKinds, k):
			servers = append(servers, api.LaneEventOf(e))
		case slices.Contains(api.LeaderLaneKinds, k):
			leaders = append(leaders, api.LaneEventOf(e))
		case slices.Contains(api.ClientLaneKinds, k):
			n := int64(1)
			if k == "client.churn" {
				n = int64(churnConnects(e))
			}
			counts = append(counts, api.ClientEventCount{Bucket: api.BucketIndex(start, bucket, at), Kind: k, Reason: e.GetReason(), N: n})
		}
	}
	for _, x := range s.restarts(start, end) {
		servers = append(servers, api.RestartLaneEvent(x))
	}
	res.Servers, res.ServersTruncated = api.NewestLane(api.TimelineLaneLimit, false, servers)
	res.Leaders, res.LeadersTruncated = api.NewestLane(api.TimelineLaneLimit, false, leaders)
	res.Clients = api.ClientLane(counts)
	var stopped bool
	res.SystemCaptureGaps, res.AdvisoryCaptureGaps, stopped = api.TimelineCaptureGaps(gaps, conns, start, end)
	if api.GapsCover(res.SystemCaptureGaps, start, end) {
		res.SystemEventsCaptured = false
	}
	if stopped {
		res.JetstreamEventsCaptured = false
	}
}

// restarts are the scenario's servers whose process, with an earlier one,
// started in [start, end) and was first collected by end.
func (s scen) restarts(start, end time.Time) []api.ServerRestart {
	var out []api.ServerRestart
	for _, sp := range s.sc.servers {
		if at := s.started(sp); sp.oldID != "" && !at.Before(start) && at.Before(end) {
			if first, _, ok := s.epochNear(sp, at, sampleEvery, time.Hour); !ok || first.After(end) {
				continue
			}
			out = append(out, api.ServerRestart{Server: sp.name, OldID: sp.oldID, NewID: sp.id, Started: at, OldVersion: sp.version, NewVersion: sp.version})
		}
	}
	return out
}
