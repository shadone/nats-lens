package fake

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// span is a stretch of time, in the fixed frame, in which a check failed.
type span struct{ from, to time.Time }

type rowSpec struct {
	code, name string
	sev        v1.Severity
	entities   []*v1.EntityRef
	spans      []span
}

func on16(h, m int) time.Time { return time.Date(2026, 10, 16, h, m, 0, 0, time.UTC) }

// prodRows are the checks of the reference timeline; open spans end at
// FixedNow and run on in live mode.
func prodRows(*scenario) []rowSpec {
	f := fixed
	servers := []*v1.EntityRef{serverRef("nats-0"), serverRef("nats-1"), serverRef("nats-2")}
	return []rowSpec{
		{"server-002", "Healthz not ok", critical, servers,
			[]span{{f(9, 16, 2), f(9, 18, 40)}, {f(9, 29, 5), f(9, 31, 40)}, {f(9, 41, 10), f(9, 44, 50)}}},
		{"server-004", "Server restarted", info, servers,
			[]span{{f(9, 18, 0), f(9, 19, 0)}, {f(9, 31, 0), f(9, 32, 0)}, {f(9, 44, 0), f(9, 45, 0)}}},
		{"stream-001", "Stream quorum lost", critical, []*v1.EntityRef{streamRef("media-windows"), streamRef("order-jobs")},
			[]span{{f(9, 16, 30), f(9, 35, 0)}, {f(9, 41, 30), f(9, 44, 30)}}},
		{"consumer-001", "Consumer quorum lost", critical, []*v1.EntityRef{consumerRef("order-jobs", "job-cache-projector")},
			[]span{{f(9, 15, 0), f(9, 45, 0)}}},
		{"stream-002", "Stream replica offline", info, []*v1.EntityRef{streamRef("order-jobs"), streamRef("media-windows")},
			[]span{{f(9, 15, 0), f(9, 50, 0)}}},
		{"stream-003", "Stream replica lagging", warning, []*v1.EntityRef{streamRef("order-archive")},
			[]span{{f(9, 16, 40), f(10, 5, 0)}}},
		{"consumer-010", "Expected consumer missing", critical, []*v1.EntityRef{consumerRef("order-archive", "analytics-aggregator")},
			[]span{{f(9, 31, 7), open}}},
		{"consumer-009", "Consumer creation time differs across servers", warning, []*v1.EntityRef{consumerRef("order-jobs", "resize-worker")},
			[]span{{f(9, 45, 0), open}}},
		{"consumer-002", "Consumer backlog growing", warning, []*v1.EntityRef{consumerRef("media-windows", "media-chunk-pool")},
			[]span{{on16(19, 0), on16(19, 30)}, {f(7, 30, 0), f(7, 45, 0)}, {f(8, 15, 0), f(8, 30, 0)}, {f(11, 0, 0), open}}},
		{"client-003", "Slow consumer disconnects", warning, []*v1.EntityRef{clientRef("media-worker")},
			[]span{{on16(22, 15), on16(22, 30)}, {f(9, 15, 0), f(9, 45, 0)}}},
		{"client-005", "Short-lived connections", warning, []*v1.EntityRef{clientRef("stock-poller")},
			[]span{{f(12, 48, 0), open}}},
		{"stream-005", "Stream without any limit", info, []*v1.EntityRef{streamRef("order-archive")},
			[]span{{time.Date(2026, 10, 14, 14, 30, 0, 0, time.UTC), open}}},
	}
}

// open is the end of a span that is still open: it runs on past now.
var open = FixedNow.Add(365 * 24 * time.Hour)

// GetStatusTimeline draws the scenario's timeline at the requested buckets.
func (p *Provider) GetStatusTimeline(_ context.Context, req *v1.GetStatusTimelineRequest) (*v1.GetStatusTimelineResponse, error) {
	s := p.scen()
	r := req.GetRange()
	n := api.BucketCount(r.GetEnd().AsTime().Sub(r.GetStart().AsTime()), req.GetBucket().AsDuration())
	res := &v1.GetStatusTimelineResponse{Start: r.GetStart(), Bucket: req.GetBucket(), BucketCount: int32(n), Windows: s.windows(r),
		Rows: s.timelineRows(r, req.GetBucket().AsDuration(), req.GetEntity(), req.GetIncludeOk())}
	res.Kubernetes, res.Nats = p.lanes(s, r)
	return res, nil
}

// timelineRows are the rows of the checks of the entity, worst first;
// without includeOK a row that never failed is left out. Buckets after the
// data's end have no data.
func (s scen) timelineRows(r *v1.TimeRange, bucket time.Duration, entity *v1.EntityRef, includeOK bool) []*v1.TimelineRow {
	start, end := r.GetStart().AsTime(), r.GetEnd().AsTime()
	n := api.BucketCount(end.Sub(start), bucket)
	type ranked struct {
		row  *v1.TimelineRow
		rank int
	}
	var rows []ranked
	for _, spec := range s.sc.rows(s.sc) {
		if !slices.ContainsFunc(spec.entities, func(e *v1.EntityRef) bool { return matchesEntity(e, entity) }) {
			continue
		}
		row := &v1.TimelineRow{Key: spec.code, CheckCode: spec.code, CheckName: spec.name, Severity: spec.sev, Cells: make([]v1.Status, n)}
		if t := entity.GetType(); t != v1.EntityType_ENTITY_TYPE_UNSPECIFIED && t != v1.EntityType_ENTITY_TYPE_SYSTEM {
			row.Entity = entity
			row.Key = spec.code + ":" + entity.GetKey()
		}
		failed := false
		for i := range n {
			b0 := start.Add(time.Duration(i) * bucket)
			b1 := earliest(b0.Add(bucket), end)
			row.Cells[i] = v1.Status_STATUS_OK
			if !b0.Before(s.dataEnd()) || !b1.After(s.dataStart()) {
				row.Cells[i] = v1.Status_STATUS_NO_DATA
			}
			for _, sp := range spec.spans {
				if sp.from.Add(s.shift).Before(b1) && sp.to.Add(s.shift).After(b0) {
					row.Cells[i], failed = statusOf(spec.sev), true
					if until, ok := s.sc.infoUntil[spec.code]; ok && !until.Add(s.shift).Before(b1) {
						row.Cells[i] = statusOf(info)
					}
				}
			}
		}
		if !failed && !includeOK {
			continue
		}
		row.Now = row.Cells[n-1]
		rows = append(rows, ranked{row, statusRank(row.Now)})
	}
	slices.SortStableFunc(rows, func(a, b ranked) int { return a.rank - b.rank })
	out := make([]*v1.TimelineRow, len(rows))
	for i, x := range rows {
		out[i] = x.row
	}
	return out
}

// lanes are the Kubernetes events of the upgrade and the NATS advisories
// and finding transitions in the range, oldest first.
func (p *Provider) lanes(s scen, r *v1.TimeRange) (k8s, nats []*v1.LaneEvent) {
	stored := map[time.Time]string{}
	for _, e := range p.events(s) {
		if e.GetSource() == srcK8s {
			stored[e.GetTime().AsTime()] = e.GetId()
		}
		if (e.GetSource() == srcNATS && e.GetClientName() != s.sc.routineClient && !routineEvent(e)) || e.GetSource() == srcFinding {
			if inRange(e.GetTime().AsTime(), r) {
				nats = append(nats, &v1.LaneEvent{Time: e.GetTime(), Source: e.GetSource(), Kind: e.GetKind(),
					ShortLabel: e.GetType(), Label: e.GetType() + " " + e.GetSummary(), Severity: e.GetSeverity(),
					Id: e.GetId(), EventId: e.GetId()})
			}
		}
	}
	lane := func(at time.Time, kind, short, label string) {
		if inRange(at, r) {
			id := stored[at]
			if id == "" {
				id = fmt.Sprintf("kubernetes:%s:%d", kind, s.clock(at).Unix())
			}
			k8s = append(k8s, &v1.LaneEvent{Id: id, Time: ts(at), Source: srcK8s, Kind: kind, ShortLabel: short, Label: label, EventId: stored[at]})
		}
	}
	if s.sc.k8sLanes != nil {
		s.sc.k8sLanes(s, lane)
	}
	slices.Reverse(nats)
	return k8s, nats
}

// prodK8sLanes are example-prod's node upgrade and the media-worker
// rollout.
func prodK8sLanes(s scen, lane func(at time.Time, kind, short, label string)) {
	lane(s.at(9, 14, 48), "k8s.node.drained", "node 7f3a", "node gke-example-pool-1-7f3a drained (upgrade 1.33.4-gke.1245)")
	lane(s.at(9, 16, 2), "k8s.pod.killing", "nats-0 Killing", "pod nats-0 Killing (node upgrade), Started 09:18")
	lane(s.at(9, 27, 0), "k8s.node.drained", "node 91c2", "node gke-example-pool-1-91c2 drained")
	lane(s.at(9, 29, 5), "k8s.pod.killing", "nats-1 Killing", "pod nats-1 Killing (node upgrade), Started 09:31")
	lane(s.at(9, 40, 0), "k8s.node.drained", "node c40d", "node gke-example-pool-1-c40d drained")
	lane(s.at(9, 41, 10), "k8s.pod.killing", "nats-2 Killing", "pod nats-2 Killing (node upgrade), Started 09:44")
	lane(s.at(9, 50, 12), "k8s.node.upgrade", "pool-1 done", "node pool pool-1 upgrade complete")
	lane(s.at(11, 2, 0), "k8s.deployment.rolled_out", "deploy", "deployment media-worker rolled out (video, 2 -> 4 replicas)")
}
