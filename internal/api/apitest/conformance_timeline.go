package apitest

import (
	"fmt"
	"slices"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// timelineRows are the states of the timeline's server, leader and client
// lanes: a restart from the collections, without an event ID, beside a
// leader election and a bucket of client events with a drop, and none of
// those kinds on the NATS lane.
func timelineRows() []State {
	return []State{
		{"a timeline with a restart, a leader election and client drops on lanes of their own", func(q Q) string {
			tl := must(q.env.Client.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{System: q.System, Range: q.day()}))).of(q.t)
			for _, e := range tl.GetNats() {
				if slices.Contains(api.ServerLaneKinds, e.GetKind()) || slices.Contains(api.LeaderLaneKinds, e.GetKind()) || e.GetKind() == api.RestartKind {
					q.t.Errorf("%s: %s on the NATS lane", q.System, e.GetKind())
				}
			}
			var restart string
			for _, e := range tl.GetServers() {
				if e.GetKind() == api.RestartKind && e.GetEventId() == "" && e.GetEntity().GetType() == v1.EntityType_ENTITY_TYPE_SERVER {
					restart = e.GetLabel()
				}
			}
			drops := int64(0)
			for _, b := range tl.GetClients() {
				if b.GetIndex() < 0 || b.GetIndex() >= tl.GetBucketCount() {
					q.t.Errorf("%s: client bucket %d outside %d buckets", q.System, b.GetIndex(), tl.GetBucketCount())
				}
				drops += b.GetDrops()
			}
			if restart == "" || len(tl.GetLeaders()) == 0 || drops == 0 || !tl.GetSystemEventsCaptured() || !tl.GetJetstreamEventsCaptured() {
				return ""
			}
			return fmt.Sprintf("%s; %d leader elections; %d drops", restart, len(tl.GetLeaders()), drops)
		}},
	}
}
