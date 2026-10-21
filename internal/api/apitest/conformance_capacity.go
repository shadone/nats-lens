package apitest

import (
	"fmt"
	"math"
	"slices"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// capacity reads the Capacity page of the system's last day.
func (q Q) capacity() *v1.GetCapacityResponse {
	q.t.Helper()
	return must(q.env.Client.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{System: q.System, Range: q.day()}))).of(q.t)
}

// capacityCard finds a server card that ok accepts.
func capacityCard(ok func(*v1.ServerStore) bool) func(Q) string {
	return func(q Q) string {
		for _, s := range q.capacity().GetServers() {
			if ok(s) {
				return s.GetServerName()
			}
		}
		return ""
	}
}

// capacityRow finds a stream row that ok accepts.
func capacityRow(ok func(*v1.CapacityStream) bool) func(Q) string {
	return func(q Q) string {
		for _, s := range q.capacity().GetStreams() {
			if ok(s) {
				return s.GetAccount() + "/" + s.GetStream()
			}
		}
		return ""
	}
}

func nextIs(limit v1.NextLimitKind, state v1.NextLimitState) func(*v1.CapacityStream) bool {
	return func(s *v1.CapacityStream) bool {
		return s.GetNextLimit().GetLimit() == limit && s.GetNextLimit().GetState() == state
	}
}

func statusIs(st v1.CapacityStatus) func(*v1.CapacityStream) bool {
	return func(s *v1.CapacityStream) bool { return s.GetStatus() == st }
}

// capacityRows are the Capacity states both providers must show.
func capacityRows() []State {
	stream := v1.CapacityStreamKind_CAPACITY_STREAM_KIND_STREAM
	return []State{
		{"a Capacity card with its time to full", capacityCard(func(s *v1.ServerStore) bool {
			return s.GetReported() && s.Used != nil && s.Limit != nil && s.GetPerDay() > 0 && s.DaysToFull != nil
		})},
		{"a Capacity card without enough history for a rate", capacityCard(func(s *v1.ServerStore) bool {
			return s.GetReported() && s.Used != nil && s.PerDay == nil && s.DaysToFull == nil
		})},
		{"a Capacity card not reported in this collection", capacityCard(func(s *v1.ServerStore) bool {
			return !s.GetReported() && s.Used == nil && s.Limit == nil && s.DaysToFull == nil
		})},
		{"a Capacity card with server-014 and the check's own days", capacityCard(func(s *v1.ServerStore) bool {
			return s.GetFindingCode() == api.CheckStoreFullSoon && s.FindingDays != nil && s.GetFindingId() != ""
		})},
		{"a Capacity row of a limits stream", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetKind() == stream && s.GetRetention() == "limits" && s.GetMirror() == "" && s.GetSources() == 0 && s.GetStorage() == "file"
		})},
		{"a Capacity row of a work queue", capacityRow(func(s *v1.CapacityStream) bool { return s.GetRetention() == "workqueue" })},
		{"a Capacity row of a mirror", capacityRow(func(s *v1.CapacityStream) bool { return s.GetMirror() != "" })},
		{"a Capacity row of a stream with sources", capacityRow(func(s *v1.CapacityStream) bool { return s.GetSources() > 0 })},
		{"a Capacity row of a KV bucket with its keys and history", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetKind() == v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE && s.Keys != nil && s.MaxMsgsPerSubject != nil
		})},
		{"a Capacity row of an object store", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetKind() == v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE && s.Keys == nil
		})},
		{"a Capacity row in memory", capacityRow(func(s *v1.CapacityStream) bool { return s.GetStorage() == "memory" })},
		{"a Capacity row whose next limit is none", capacityRow(nextIs(v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0))},
		{"a Capacity row that is empty", capacityRow(func(s *v1.CapacityStream) bool {
			return nextIs(v1.NextLimitKind_NEXT_LIMIT_KIND_EMPTY, 0)(s) && s.FirstTs == nil && s.GetMessages() == 0 && s.Messages != nil
		})},
		{"a Capacity row steady at its age limit", capacityRow(nextIs(v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY))},
		{"a Capacity row at a limit that refuses new messages", capacityRow(func(s *v1.CapacityStream) bool {
			n := s.GetNextLimit()
			return n.GetState() == v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING && n.GetDiscardNew() && s.GetDiscard() == "new"
		})},
		{"a Capacity row that reaches a limit in days", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetNextLimit().GetState() == v1.NextLimitState_NEXT_LIMIT_STATE_REACHES && s.GetNextLimit().Days != nil
		})},
		{"a Capacity row that does not reach its limit", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetNextLimit().GetState() == v1.NextLimitState_NEXT_LIMIT_STATE_NOT_REACHED && s.GetNextLimit().Days == nil
		})},
		{"a Capacity row with a per-subject limit alone, not forecast",
			capacityRow(nextIs(v1.NextLimitKind_NEXT_LIMIT_KIND_PER_SUBJECT, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_FORECAST))},
		{"a Capacity row that fills soon, with stream-006's days", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetStatus() == v1.CapacityStatus_CAPACITY_STATUS_FILLS_SOON && s.FindingDays != nil && s.GetFindingId() != ""
		})},
		{"a Capacity row accepted as by design", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetStatus() == v1.CapacityStatus_CAPACITY_STATUS_ACCEPTED && s.GetMute().GetAccepted() && s.GetMute().GetReason() != "" &&
				s.GetMute().GetExpiresAt() != nil
		})},
		{"a Capacity row declared unbounded", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetStatus() == v1.CapacityStatus_CAPACITY_STATUS_DECLARED && s.GetDeclaredReason() != "" && s.GetFindingId() == ""
		})},
		{"a Capacity row whose stream-005 is muted", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetStatus() == v1.CapacityStatus_CAPACITY_STATUS_MUTED && s.GetMute() != nil && !s.GetMute().GetAccepted()
		})},
		{"a Capacity row awaiting a decision", capacityRow(func(s *v1.CapacityStream) bool {
			return s.GetStatus() == v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION && s.GetMute() == nil && s.GetFindingId() != ""
		})},
		{"a Capacity row bounded", capacityRow(statusIs(v1.CapacityStatus_CAPACITY_STATUS_BOUNDED))},
		{"a Capacity page whose streams are those ListStreams lists, with the same growth, and at most 500 of its total", everySystem(func(q Q) string {
			res := q.capacity()
			if len(res.GetStreams()) > api.MaxCapacityRows || int(res.GetStreamsTotal()) < len(res.GetStreams()) ||
				(res.GetStreamsTotal() <= api.MaxCapacityRows && int(res.GetStreamsTotal()) != len(res.GetStreams())) {
				q.t.Errorf("%s: %d rows of %d", q.System, len(res.GetStreams()), res.GetStreamsTotal())
				return ""
			}
			list := map[string]*v1.StreamSummary{}
			for _, s := range q.streams(q.day()) {
				list[s.GetEntity().GetKey()] = s
			}
			var keys []string
			for _, s := range res.GetStreams() {
				key := s.GetAccount() + "/" + s.GetStream()
				keys = append(keys, key)
				l := list[key]
				if l == nil {
					q.t.Errorf("%s: %s is on the Capacity page and not in ListStreams", q.System, key)
					return ""
				}
				if (l.BytesPerDay == nil) != (s.BytesPerDay == nil) || math.Abs(l.GetBytesPerDay()-s.GetBytesPerDay()) > 1e-6*math.Abs(l.GetBytesPerDay()) {
					q.t.Errorf("%s %s: growth %v on the Capacity page, %v in ListStreams", q.System, key, s.BytesPerDay, l.BytesPerDay)
					return ""
				}
			}
			if res.GetStreamsTotal() <= api.MaxCapacityRows && len(keys) != len(list) {
				q.t.Errorf("%s: %d Capacity rows, %d streams listed", q.System, len(keys), len(list))
				return ""
			}
			if res.GetEpoch() == 0 {
				return ""
			}
			slices.Sort(keys)
			return fmt.Sprintf("%d streams", len(keys))
		})},
	}
}
