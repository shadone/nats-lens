package api

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Lanes of the timeline besides Kubernetes and the NATS lane: each kind is
// drawn on one lane only.
var (
	// ServerLaneKinds are the stored events of the server lane; the
	// restarts come from the collections (ServerRestart).
	ServerLaneKinds = []string{"server.lameduck", "server.shutdown", "server.out_of_storage", "server.removed"}
	// LeaderLaneKinds are the leader elections.
	LeaderLaneKinds = []string{"meta.leader_elected", "stream.leader_elected", "consumer.leader_elected"}
	// ClientLaneKinds are the client events the client lane counts.
	ClientLaneKinds = []string{"client.connect", "client.disconnect", "client.churn", "client.auth_error"}
)

// RestartKind is the kind of a server lane item for a restart; it is no
// stored event.
const RestartKind = "server.restarted"

// TimelineLaneLimit is the most items one event lane of the timeline holds.
const TimelineLaneLimit = 1000

// LaneEventOf is a stored event as the NATS, server and leader lanes draw
// it.
func LaneEventOf(ev *v1.Event) *v1.LaneEvent {
	return &v1.LaneEvent{Id: ev.GetId(), Time: ev.GetTime(), Source: ev.GetSource(), Kind: ev.GetKind(),
		ShortLabel: ev.GetType(), Label: ev.GetType() + " " + ev.GetSummary(), Severity: ev.GetSeverity(), EventId: ev.GetId(),
		Entity: EventEntity(ev), FindingId: ev.GetTransition().GetFindingId(), TransitionEpoch: ev.GetTransition().GetEpoch()}
}

// ServerRestart is a restart seen in the collections: a process of a
// server name with an earlier one, started at Started (its reported start
// time, or when it was first collected).
type ServerRestart struct {
	Server, OldID, NewID   string
	Started                time.Time
	OldVersion, NewVersion string
}

// RestartMatches reports whether a restart of server belongs to a
// timeline for entity: every restart for the system, the server's own for
// a server, none for other entities, as the event lanes filter.
func RestartMatches(entity *v1.EntityRef, server string) bool {
	switch entity.GetType() {
	case v1.EntityType_ENTITY_TYPE_UNSPECIFIED, v1.EntityType_ENTITY_TYPE_SYSTEM:
		return true
	case v1.EntityType_ENTITY_TYPE_SERVER:
		return entity.GetKey() == server
	}
	return false
}

// RestartLaneEvent is the server lane item of a restart. It has no event
// ID: the restart is derived from the collections.
func RestartLaneEvent(r ServerRestart) *v1.LaneEvent {
	label := r.Server + " restarted, version " + cmp.Or(r.NewVersion, "unknown") + " unchanged"
	if r.OldVersion != r.NewVersion {
		label = r.Server + " restarted, version " + cmp.Or(r.OldVersion, "unknown") + " to " + cmp.Or(r.NewVersion, "unknown")
	}
	return &v1.LaneEvent{Id: "restart:" + r.NewID, Time: ts(r.Started.UTC()), Source: v1.EventSource_EVENT_SOURCE_NATS, Kind: RestartKind,
		ShortLabel: r.Server + " restart", Label: label, Entity: EntityRefOf(v1.EntityType_ENTITY_TYPE_SERVER, r.Server)}
}

// NewestLane merges lane items, keeps the newest limit and returns them
// oldest first, with whether any was left out: cut says a part was cut
// already.
func NewestLane(limit int, cut bool, parts ...[]*v1.LaneEvent) ([]*v1.LaneEvent, bool) {
	all := slices.Concat(parts...)
	slices.SortStableFunc(all, func(a, b *v1.LaneEvent) int {
		if c := a.GetTime().AsTime().Compare(b.GetTime().AsTime()); c != 0 {
			return c
		}
		ai, aerr := strconv.ParseInt(a.GetId(), 10, 64)
		bi, berr := strconv.ParseInt(b.GetId(), 10, 64)
		if aerr == nil && berr == nil {
			return cmp.Compare(ai, bi)
		}
		return cmp.Compare(a.GetId(), b.GetId())
	})
	if len(all) > limit {
		return all[len(all)-limit:], true
	}
	return all, cut
}

// ClientEventCount is n client events of one kind (and, for a
// disconnect, one reason) in one bucket; for client.churn n is the
// connects it folded.
type ClientEventCount struct {
	Bucket       int
	Kind, Reason string
	N            int64
}

// ClientLane counts client events per bucket for the client lane: only
// buckets with a count, oldest first. A churn summary counts its folded
// connects as connects and as client closes.
func ClientLane(counts []ClientEventCount) []*v1.ClientLaneBucket {
	byIndex := map[int]*v1.ClientLaneBucket{}
	for _, c := range counts {
		if c.N <= 0 {
			continue
		}
		b := byIndex[c.Bucket]
		if b == nil {
			b = &v1.ClientLaneBucket{Index: int32(c.Bucket)}
			byIndex[c.Bucket] = b
		}
		switch c.Kind {
		case "client.connect":
			b.Connects += c.N
		case "client.churn":
			b.Connects += c.N
			b.Closes += c.N
		case "client.auth_error":
			b.AuthErrors += c.N
		case "client.disconnect":
			switch c.Reason {
			case "Client Closed":
				b.Closes += c.N
			case "Server Shutdown":
				b.Shutdowns += c.N
			default:
				b.Drops += c.N
			}
		}
	}
	out := make([]*v1.ClientLaneBucket, 0, len(byIndex))
	for _, b := range byIndex {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b *v1.ClientLaneBucket) int { return cmp.Compare(a.GetIndex(), b.GetIndex()) })
	return out
}

// BucketIndex is the bucket of t: whole buckets from start.
func BucketIndex(start time.Time, bucket time.Duration, t time.Time) int {
	if bucket <= 0 {
		return 0
	}
	return int(t.Sub(start) / bucket)
}

// SystemEventKind reports whether kind is a NATS system event (client,
// server and account events) rather than a JetStream advisory.
func SystemEventKind(kind string) bool {
	for _, p := range []string{"stream.", "consumer.", "meta.", "server.out_of_storage", "server.removed"} {
		if strings.HasPrefix(kind, p) {
			return false
		}
	}
	return true
}
