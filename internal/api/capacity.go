package api

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Capacity limits and rules.
const (
	// MaxCapacityRows is the most streams a GetCapacity response lists.
	MaxCapacityRows = 500
	// AgeLimitReached is the share of max_age at which the oldest message
	// counts as at the limit: age is measured at collections up to one
	// interval apart.
	AgeLimitReached = 0.95
	// CountLimitReached is the share of max_msgs or max_bytes at which a
	// stream counts as at the limit: the server enforces those exactly.
	CountLimitReached = 0.99
)

// Check codes the Capacity page reads.
const (
	CheckUnboundedStream = "stream-005"
	CheckStreamBytesSoon = "stream-006"
	CheckStoreHigh       = "server-007"
	CheckStoreFullSoon   = "server-014"
)

// Reasons a stream is declared unbounded on purpose.
const (
	DeclaredInventory  = "inventory"
	DeclaredWriterTTLs = "writer_ttls"
)

// CapacityFacts are what a provider read for one GetCapacity: the epoch's
// server stores and streams with their growth over the range, and the
// firing findings with their mutes, without order or limits.
// FinishCapacity turns them into the response, so the store and the demo
// apply the same rules.
type CapacityFacts struct {
	// Epoch is the range's newest epoch; 0 when the range holds none.
	Epoch   int64
	EpochAt time.Time
	// GrowthFrom is the first collection the growth reads.
	GrowthFrom time.Time
	// Servers are the servers the epoch expected that have JetStream or
	// did not answer; ServersWithoutJetStream counts those that answered
	// without it.
	Servers                 []ServerStoreFact
	ServersWithoutJetStream int32
	Streams                 []CapacityStreamFact
	// Findings are the findings firing in the epoch, with the mute or
	// acceptance in force on each; only stream-005, stream-006, server-007
	// and server-014 are read.
	Findings []*v1.Finding
}

// ServerStoreFact is one server's file store.
type ServerStoreFact struct {
	Name, Cluster string
	// Reported is whether its JSZ answered in the epoch.
	Reported bool
	// Used and Limit are nil when not reported; a Limit of 0 is none.
	Used, Limit *int64
	// PerDay is RobustPerDay of the store in use over the range.
	PerDay *float64
}

// CapacityStreamFact is one stream of the epoch, with ListStreams' numbers.
type CapacityStreamFact struct {
	Account, Stream            string
	Retention, Storage, Mirror string
	Discard                    string
	Replicas, Sources          int32
	// Messages, Bytes and Keys are nil when not reported; Keys is read for
	// KV buckets only.
	Messages, Bytes, Keys *int64
	// FirstTS is nil for an empty stream.
	FirstTS *time.Time
	// The limits as the server reports them: -1 or 0 is none.
	MaxMsgs, MaxBytes, MaxMsgsPerSubject *int64
	MaxAge                               *time.Duration
	AllowMsgTTL                          bool
	// BytesPerDay and MsgsPerDay are RobustPerDay of the leader's bytes
	// and messages over the range.
	BytesPerDay, MsgsPerDay *float64
	// Declared is DeclaredInventory or DeclaredWriterTTLs when the
	// inventory makes stream-005 leave the stream out; empty otherwise.
	Declared string
}

// FinishCapacity builds the response from the facts: the server cards with
// their time to full in order, and the streams with their next limit and
// status, in order and cut at MaxCapacityRows.
func FinishCapacity(f *CapacityFacts) *v1.GetCapacityResponse {
	if f == nil || f.Epoch == 0 {
		return &v1.GetCapacityResponse{}
	}
	res := &v1.GetCapacityResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt), ServersWithoutJetstream: f.ServersWithoutJetStream}
	if !f.GrowthFrom.IsZero() {
		res.GrowthFrom = timestamppb.New(f.GrowthFrom)
	}
	firing := map[string]*v1.Finding{}
	for _, x := range f.Findings {
		if x.GetState() == v1.FindingState_FINDING_STATE_RESOLVED {
			continue
		}
		firing[x.GetCheckCode()+":"+x.GetEntity().GetKey()] = x
	}
	for _, s := range f.Servers {
		res.Servers = append(res.Servers, serverStore(s, firing))
	}
	slices.SortStableFunc(res.Servers, compareServerStores)
	for _, s := range f.Streams {
		res.Streams = append(res.Streams, capacityStream(f.EpochAt, s, firing))
	}
	slices.SortStableFunc(res.Streams, compareCapacityStreams)
	res.StreamsTotal = int32(len(res.Streams))
	res.Streams = res.Streams[:min(len(res.Streams), MaxCapacityRows)]
	return res
}

func serverStore(s ServerStoreFact, firing map[string]*v1.Finding) *v1.ServerStore {
	out := &v1.ServerStore{ServerName: s.Name, Cluster: s.Cluster, Reported: s.Reported}
	// A card not reported in the epoch shows no rate: its growth would be
	// of collections before it, beside no use to set it against.
	if s.Reported {
		out.Used, out.PerDay = s.Used, s.PerDay
		if s.Limit != nil && *s.Limit > 0 {
			out.Limit = s.Limit
		}
	}
	out.DaysToFull = DaysToFull(out.Used, out.Limit, out.PerDay)
	fd := firing[CheckStoreFullSoon+":"+s.Name]
	if fd == nil {
		fd = firing[CheckStoreHigh+":"+s.Name]
	}
	if fd != nil {
		out.FindingId, out.FindingCode, out.FindingSeverity = fd.GetId(), fd.GetCheckCode(), fd.GetSeverity()
		if fd.GetCheckCode() == CheckStoreFullSoon && fd.GetValue().Number != nil {
			out.FindingDays = ptrTo(fd.GetValue().GetNumber())
		}
	}
	return out
}

// DaysToFull is the days until used reaches limit at perDay; nil unless
// all three are known and perDay is above 0.
func DaysToFull(used, limit *int64, perDay *float64) *float64 {
	if used == nil || limit == nil || perDay == nil || *perDay <= 0 {
		return nil
	}
	return ptrTo(max(float64(*limit-*used), 0) / *perDay)
}

// CapacityKind is the kind of a stream by the prefix rule of
// ListKeyValueStores and ListObjectStores.
func CapacityKind(stream string) v1.CapacityStreamKind {
	switch {
	case strings.HasPrefix(stream, "KV_"):
		return v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE
	case strings.HasPrefix(stream, "OBJ_"):
		return v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE
	}
	return v1.CapacityStreamKind_CAPACITY_STREAM_KIND_STREAM
}

// positive keeps a limit that is set: above 0.
func positive(v *int64) *int64 {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

func capacityStream(epochAt time.Time, s CapacityStreamFact, firing map[string]*v1.Finding) *v1.CapacityStream {
	kind := CapacityKind(s.Stream)
	out := &v1.CapacityStream{
		Account: s.Account, Stream: s.Stream, Kind: kind, Retention: s.Retention, Storage: s.Storage, Replicas: s.Replicas,
		Mirror: s.Mirror, Sources: s.Sources, Messages: s.Messages, Bytes: s.Bytes, MaxMsgs: positive(s.MaxMsgs),
		MaxBytes: positive(s.MaxBytes), MaxMsgsPerSubject: positive(s.MaxMsgsPerSubject), Discard: s.Discard,
		AllowMsgTtl: s.AllowMsgTTL, BytesPerDay: s.BytesPerDay, MsgsPerDay: s.MsgsPerDay,
	}
	if kind == v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE {
		out.Keys = s.Keys
	}
	if s.FirstTS != nil && !s.FirstTS.IsZero() {
		out.FirstTs = timestamppb.New(*s.FirstTS)
	}
	if s.MaxAge != nil && *s.MaxAge > 0 {
		out.MaxAge = durationpb.New(*s.MaxAge)
	}
	out.NextLimit = NextLimitOf(epochAt, out)
	key := s.Account + "/" + s.Stream
	unbounded := out.MaxMsgs == nil && out.MaxBytes == nil && out.MaxAge == nil
	switch f005, f006 := firing[CheckUnboundedStream+":"+key], firing[CheckStreamBytesSoon+":"+key]; {
	case f006 != nil:
		out.Status, out.FindingId, out.Mute = v1.CapacityStatus_CAPACITY_STATUS_FILLS_SOON, f006.GetId(), f006.GetMute()
		if f006.GetValue().Number != nil {
			out.FindingDays = ptrTo(f006.GetValue().GetNumber())
		}
	case f005 != nil:
		out.FindingId, out.Mute = f005.GetId(), f005.GetMute()
		switch {
		case f005.GetMute().GetAccepted():
			out.Status = v1.CapacityStatus_CAPACITY_STATUS_ACCEPTED
		case f005.GetMute() != nil:
			out.Status = v1.CapacityStatus_CAPACITY_STATUS_MUTED
		default:
			out.Status = v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION
		}
	case unbounded && s.Declared != "":
		out.Status, out.DeclaredReason = v1.CapacityStatus_CAPACITY_STATUS_DECLARED, s.Declared
	case unbounded:
		out.Status = v1.CapacityStatus_CAPACITY_STATUS_UNBOUNDED
	default:
		out.Status = v1.CapacityStatus_CAPACITY_STATUS_BOUNDED
	}
	return out
}

// NextLimitOf is the limit a stream reaches next, from its limits (set
// only when above 0), the leader's numbers and their growth: empty; a
// limit reached now (the oldest message at AgeLimitReached of max_age,
// messages or bytes at CountLimitReached of theirs), one that refuses
// messages first, then in the order age, messages, bytes; else the soonest forecast; else the first limit set,
// not reached; else a per-subject limit alone, not forecast; else none.
func NextLimitOf(epochAt time.Time, s *v1.CapacityStream) *v1.NextLimit {
	if s.Messages != nil && s.GetMessages() == 0 {
		return &v1.NextLimit{Limit: v1.NextLimitKind_NEXT_LIMIT_KIND_EMPTY}
	}
	discardNew := s.GetDiscard() == "new"
	var held *time.Duration
	if s.GetFirstTs() != nil {
		held = ptrTo(epochAt.Sub(s.GetFirstTs().AsTime()))
	}
	type limit struct {
		kind    v1.NextLimitKind
		reached bool
		days    *float64
	}
	var set []limit
	if s.GetMaxAge() != nil {
		maxAge := s.GetMaxAge().AsDuration()
		l := limit{kind: v1.NextLimitKind_NEXT_LIMIT_KIND_AGE}
		if held != nil {
			l.reached = float64(*held) >= AgeLimitReached*float64(maxAge)
			l.days = ptrTo(max(maxAge-*held, 0).Hours() / 24)
		}
		set = append(set, l)
	}
	counted := func(kind v1.NextLimitKind, max *int64, now *int64, perDay *float64) {
		if max == nil {
			return
		}
		l := limit{kind: kind}
		if now != nil {
			l.reached = float64(*now) >= CountLimitReached*float64(*max)
			if perDay != nil && *perDay > 0 {
				l.days = ptrTo(float64(*max-*now) / *perDay)
			}
		}
		set = append(set, l)
	}
	counted(v1.NextLimitKind_NEXT_LIMIT_KIND_MESSAGES, s.MaxMsgs, s.Messages, s.MsgsPerDay)
	counted(v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, s.MaxBytes, s.Bytes, s.BytesPerDay)
	refuses := func(k v1.NextLimitKind) bool { return discardNew && k != v1.NextLimitKind_NEXT_LIMIT_KIND_AGE }
	// A limit that refuses messages counts before one that drops the oldest.
	for _, l := range set {
		if l.reached && refuses(l.kind) {
			return &v1.NextLimit{Limit: l.kind, State: v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING, DiscardNew: true}
		}
	}
	for _, l := range set {
		if l.reached {
			return &v1.NextLimit{Limit: l.kind, State: v1.NextLimitState_NEXT_LIMIT_STATE_STEADY}
		}
	}
	var soonest *limit
	for i, l := range set {
		if l.days != nil && (soonest == nil || *l.days < *soonest.days) {
			soonest = &set[i]
		}
	}
	switch {
	case soonest != nil:
		return &v1.NextLimit{Limit: soonest.kind, State: v1.NextLimitState_NEXT_LIMIT_STATE_REACHES, Days: soonest.days,
			DiscardNew: refuses(soonest.kind)}
	case len(set) > 0:
		return &v1.NextLimit{Limit: set[0].kind, State: v1.NextLimitState_NEXT_LIMIT_STATE_NOT_REACHED, DiscardNew: refuses(set[0].kind)}
	case s.MaxMsgsPerSubject != nil:
		return &v1.NextLimit{Limit: v1.NextLimitKind_NEXT_LIMIT_KIND_PER_SUBJECT, State: v1.NextLimitState_NEXT_LIMIT_STATE_NOT_FORECAST}
	}
	return &v1.NextLimit{Limit: v1.NextLimitKind_NEXT_LIMIT_KIND_NONE}
}

// compareServerStores groups the cards by cluster in name order, then
// puts the soonest full first in whole days, then the fullest, then orders
// by name. Days are rounded so that stores whose growth differs only by
// floating-point noise keep their name order from one answer to the next.
func compareServerStores(a, b *v1.ServerStore) int {
	days := func(s *v1.ServerStore) *float64 {
		if s.DaysToFull == nil {
			return nil
		}
		d := math.Round(s.GetDaysToFull())
		return &d
	}
	share := func(s *v1.ServerStore) float64 {
		if s.Used == nil || s.Limit == nil {
			return -1
		}
		return float64(s.GetUsed()) / float64(s.GetLimit())
	}
	return cmp.Or(cmp.Compare(a.GetCluster(), b.GetCluster()), compareSoonest(days(a), days(b)),
		cmp.Compare(share(b), share(a)), cmp.Compare(a.GetServerName(), b.GetServerName()))
}

// compareSoonest orders known days first, the fewest first.
func compareSoonest(a, b *float64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return cmp.Compare(*a, *b)
}

// compareCapacityStreams puts the streams that fill soon first (soonest
// first), then those awaiting a decision, then those that fill soon under
// a mute (soonest first), then the others by bytes times replicas, most
// first, then by account and name.
func compareCapacityStreams(a, b *v1.CapacityStream) int {
	rank := func(s *v1.CapacityStream) int {
		switch s.GetStatus() {
		case v1.CapacityStatus_CAPACITY_STATUS_FILLS_SOON:
			if s.GetMute() != nil {
				return 2
			}
			return 0
		case v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION:
			return 1
		}
		return 3
	}
	size := func(s *v1.CapacityStream) int64 { return s.GetBytes() * int64(max(s.GetReplicas(), 1)) }
	return cmp.Or(cmp.Compare(rank(a), rank(b)), compareSoonest(a.FindingDays, b.FindingDays), cmp.Compare(size(b), size(a)),
		cmp.Compare(a.GetAccount(), b.GetAccount()), cmp.Compare(a.GetStream(), b.GetStream()))
}
