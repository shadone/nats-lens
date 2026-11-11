package api

import (
	"cmp"
	"encoding/json"
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
	// FirstCollected says Started is when the process was first collected,
	// as it reported no start time.
	FirstCollected bool
}

// SubjectLanesFor reports whether a timeline for entity has the server,
// leader and client lanes: only the whole system's has.
func SubjectLanesFor(entity *v1.EntityRef) bool {
	t := entity.GetType()
	return t == v1.EntityType_ENTITY_TYPE_UNSPECIFIED || t == v1.EntityType_ENTITY_TYPE_SYSTEM
}

// CaptureLookback is how far before a timeline's end a stored event of a
// source counts as that source being captured, when the range is shorter.
const CaptureLookback = 7 * 24 * time.Hour

// CaptureSince is where a timeline looks for a stored event of a source:
// the range's start, or CaptureLookback before its end when that is
// earlier.
func CaptureSince(start, end time.Time) time.Time {
	if s := end.Add(-CaptureLookback); s.Before(start) {
		return s
	}
	return start
}

// CaptureGapSpans are the event capture gaps (GapCollector and GapEvents)
// clipped to [start, end) and merged, oldest first; an open gap runs to
// end.
func CaptureGapSpans(gaps []ReplayGap, start, end time.Time) []*v1.TimeRange {
	type span struct{ from, to time.Time }
	var spans []span
	for _, g := range gaps {
		if g.Kind != GapCollector && g.Kind != GapEvents {
			continue
		}
		from, to := g.Start, g.End
		if to.IsZero() || to.After(end) {
			to = end
		}
		if from.Before(start) {
			from = start
		}
		if to.After(from) {
			spans = append(spans, span{from, to})
		}
	}
	slices.SortFunc(spans, func(a, b span) int { return a.from.Compare(b.from) })
	var out []*v1.TimeRange
	for _, x := range spans {
		if n := len(out); n > 0 && !x.from.After(out[n-1].GetEnd().AsTime()) {
			if x.to.After(out[n-1].GetEnd().AsTime()) {
				out[n-1].End = ts(x.to)
			}
			continue
		}
		out = append(out, &v1.TimeRange{Start: ts(x.from), End: ts(x.to)})
	}
	return out
}

// SystemConnection is the label of the connection that captures the NATS
// system events; the collector's own is "collector", the others capture
// JetStream advisories of one account each.
const SystemConnection = "sys"

// TimelineCaptureGaps splits the event capture gaps of [start, end) by
// what they stopped: collector holds the collector.gap gaps (GapCollector;
// others are ignored), conns the connection events oldest first, as
// EventsConnectionGaps takes them. system are the spans without system
// events: collector gaps and the system connection's. advisory are the
// collector gaps (no connection) and each account connection's gaps.
// collectorCovers says nats-lens was not running for the whole range.
func TimelineCaptureGaps(collector []ReplayGap, conns []*v1.Event, start, end time.Time) (system []*v1.TimeRange, advisory []*v1.CaptureGap, collectorCovers bool) {
	groups := map[string][]*v1.Event{}
	var labels []string
	for _, e := range conns {
		if e.GetKind() == "collector.started" {
			for l := range groups {
				groups[l] = append(groups[l], e)
			}
			continue
		}
		var raw struct {
			Connection string `json:"connection"`
		}
		_ = json.Unmarshal([]byte(e.GetRaw()), &raw)
		if raw.Connection == "" || raw.Connection == "collector" {
			continue
		}
		if _, ok := groups[raw.Connection]; !ok {
			labels = append(labels, raw.Connection)
		}
		groups[raw.Connection] = append(groups[raw.Connection], e)
	}
	stopped := CaptureGapSpans(collector, start, end)
	collectorCovers = GapsCover(stopped, start, end)
	system = CaptureGapSpans(append(slices.Clone(collector), EventsConnectionGaps(groups[SystemConnection])...), start, end)
	for _, r := range stopped {
		advisory = append(advisory, &v1.CaptureGap{Range: r})
	}
	slices.Sort(labels)
	for _, l := range labels {
		if l == SystemConnection {
			continue
		}
		for _, r := range CaptureGapSpans(EventsConnectionGaps(groups[l]), start, end) {
			advisory = append(advisory, &v1.CaptureGap{Range: r, Connection: l})
		}
	}
	slices.SortStableFunc(advisory, func(a, b *v1.CaptureGap) int {
		return a.GetRange().GetStart().AsTime().Compare(b.GetRange().GetStart().AsTime())
	})
	return system, advisory, collectorCovers
}

// GapsCover reports whether merged spans cover all of [start, end).
func GapsCover(spans []*v1.TimeRange, start, end time.Time) bool {
	for _, x := range spans {
		if !x.GetStart().AsTime().After(start) && !x.GetEnd().AsTime().Before(end) {
			return true
		}
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
	if r.FirstCollected {
		label += "; no start time reported, first collected then"
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
			case "":
				b.UnknownDisconnects += c.N
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
