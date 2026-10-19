package api

import (
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Replay limits.
const (
	// MaxReplaySpan is the longest time between A and B.
	MaxReplaySpan = 24 * time.Hour
	// ReplayLookahead is how far after B a step that closes a step in the
	// range is looked for.
	ReplayLookahead = 6 * time.Hour
	// ReplayEpochReach is how many collect intervals before a moment the
	// collection read as the state at that moment may have started.
	ReplayEpochReach = 3

	maxReplaySteps  = 200
	maxOutsideSteps = 10
	maxStepEvidence = 10
	maxStepEntities = 50
	maxGoneAssets   = 200
)

// Event kinds a provider reads for Replay, from ReplayReadFrom to
// ReplayFacts.Until.
var (
	// ReplayK8sStepKinds are Kubernetes events that make a step of their
	// own; pod kinds only for NATS pods (with a server name).
	ReplayK8sStepKinds = []string{"k8s.node.drained", "k8s.node.upgrade", "k8s.pod.failed_scheduling", "k8s.pod.backoff",
		"k8s.pod.oom_killed"}
	// ReplayPodKinds are NATS pod events that go into a restart or stop
	// step as evidence, or make a Kubernetes step outside one.
	ReplayPodKinds = []string{"k8s.pod.killing", "k8s.pod.started"}
	// ReplayNATSKinds are the NATS events Replay reads: announcements,
	// leader elections and quorum advisories.
	ReplayNATSKinds = []string{"server.lameduck", "server.shutdown", "stream.leader_elected", "consumer.leader_elected",
		"meta.leader_elected", "stream.quorum_lost", "consumer.quorum_lost"}
	// ReplayGoneChecks are the checks whose findings about a gone asset
	// go into its step and its finding_ids.
	ReplayGoneChecks = []string{"consumer-013", "stream-014", "consumer-010", "stream-011"}
)

// ReplayReadFrom is where a provider reads the events and collector gaps
// from: the restart window before the earlier of A and A's collection.
func ReplayReadFrom(f *ReplayFacts) time.Time {
	from := f.A
	if f.EpochA != 0 && f.EpochAAt.Before(from) {
		from = f.EpochAAt
	}
	return from.Add(-f.RestartWindow)
}

// ReplayEventKinds are every event kind of the lists above.
func ReplayEventKinds() []string {
	out := append([]string{}, ReplayK8sStepKinds...)
	out = append(out, ReplayPodKinds...)
	return append(out, ReplayNATSKinds...)
}

// quorumChecks are the checks whose findings make quorum steps.
var quorumChecks = map[string]bool{"stream-001": true, "consumer-001": true, "system-002": true}

// restartCheck is the check whose transitions a restart step takes in.
const restartCheck = "server-004"

// ReplayFacts are what a provider read for one Replay: the facts only,
// without wording. FinishReplay turns them into the response, so the
// store and the demo word, fold and count alike.
type ReplayFacts struct {
	// A and B are the moments, B already clamped to now. Until is the end
	// of the lookahead after B: the earlier of now and B plus
	// ReplayLookahead.
	A, B, Until time.Time
	// Interval is the collect interval; RestartWindow how long before a
	// process stopped its announcement may come.
	Interval, RestartWindow time.Duration
	// EpochA and EpochB are the collections read as the state at A and B
	// (the newest that started at or before the moment, within
	// ReplayEpochReach intervals), 0 when none, with their start times.
	EpochA, EpochB     int64
	EpochAAt, EpochBAt time.Time
	// HistoryStart is the start of the stored history.
	HistoryStart time.Time
	// Collected is whether a collection started between A and B;
	// EventsCaptured whether events were captured (any stored event up to
	// B); Kubernetes whether nats-lens watches Kubernetes. FinishReplay
	// treats event capture and the Kubernetes watch as broken over A to B
	// when a GapCollector or GapKubernetes gap overlaps it.
	Collected, EventsCaptured, Kubernetes bool

	// Restarts are the processes that replaced an earlier one of the same
	// server name, started after A and at or before B, and were first
	// collected by B; one first collected after B makes its server a stop.
	Restarts []ReplayRestart
	// Stops are the servers that answered in A's collection or later and
	// did not answer at B.
	Stops []ReplayStop
	// Events are the events of ReplayEventKinds from ReadFrom to Until, in
	// any order; pod kinds only of NATS pods.
	Events []*v1.Event
	// ReadFrom is where the events and the collector gaps were read from:
	// the restart window before the earlier of A and A's collection, so
	// that a stop last seen in A's collection has its whole announcement
	// window read. Nothing is claimed absent before it.
	ReadFrom time.Time
	// Transitions are the finding transitions from A to Until, and for
	// each finding still firing at A the newest before A and the firing
	// that started it.
	Transitions []ReplayTransition
	// EventsTruncated and TransitionsTruncated say that a provider's read
	// limit cut Events or Transitions: the steps are then marked truncated
	// and the counts taken from them stay unset.
	EventsTruncated, TransitionsTruncated bool
	// EventsReadUntil and TransitionsReadUntil are, when cut, the time of
	// the last row read: rows before it were read in full.
	EventsReadUntil, TransitionsReadUntil time.Time
	// LeadersAtA are the leaders of the streams and consumers at EpochA;
	// MetaLeaderAtA the meta leader most servers named then.
	LeadersAtA    []ReplayLeader
	MetaLeaderAtA string
	// Windows are the Kubernetes windows overlapping A to B.
	Windows []*v1.TimeWindow
	// Gone are the assets servers held at EpochA and none holds at EpochB;
	// GoneRead is whether they were read, so that the gone counts stay
	// unset when they were not.
	Gone     []ReplayGone
	GoneRead bool
	// Gaps are the times without collections that overlap A to B, and the
	// times nats-lens was not running or Kubernetes was not watched that
	// overlap ReadFrom to B (they decide whether a step may say an
	// announcement was not captured).
	Gaps []ReplayGap
	// ClientReconnects counts reconnects between A and B (see
	// ReplayCounts.client_reconnects); read only with EventsCaptured.
	ClientReconnects int32
	// Nodes counts the Kubernetes node changes between A and B; read only
	// with Kubernetes.
	Nodes ReplayNodes
}

// ReplayRestart is one process replaced by another.
type ReplayRestart struct {
	Server       string
	OldID, NewID string
	// LastSeen is when the old process last answered, in epoch OldEpoch;
	// FirstSeen when the new one first did, in epoch NewEpoch; Started the
	// new process's start time (FirstSeen when not reported).
	LastSeen, FirstSeen, Started time.Time
	OldEpoch, NewEpoch           int64
	OldVersion, NewVersion       string
	// LedBefore are the streams and consumers the old process led in
	// OldEpoch.
	LedBefore []*v1.EntityRef
	// StreamsBefore and ConsumersBefore are what the old process held in
	// OldEpoch, StreamsAfter and ConsumersAfter what the new one held in
	// NewEpoch; nil when the server did not answer JSZ then.
	StreamsBefore, ConsumersBefore, StreamsAfter, ConsumersAfter *int32
}

// ReplayStop is a server that answered after A and not at B.
type ReplayStop struct {
	Server string
	// LastSeen is when it last answered, in epoch LastEpoch.
	LastSeen  time.Time
	LastEpoch int64
	// BackAt is when a process of the server answered again after B, up
	// to Until, in epoch BackEpoch; zero when none did.
	BackAt    time.Time
	BackEpoch int64
}

// ReplayTransition is one transition of a finding.
type ReplayTransition struct {
	FindingID string
	Check     string
	// CheckName is the check's name; empty when unknown.
	CheckName string
	Entity    *v1.EntityRef
	Time      time.Time
	Epoch     int64
	State     v1.FindingState
	Severity  v1.Severity
	Detail    string
}

// ReplayLeader is the leader of a stream or consumer at A.
type ReplayLeader struct {
	// Entity carries account_name when known, so that an event naming
	// the account by name matches it.
	Entity *v1.EntityRef
	Server string
}

// ReplayGone is an asset servers held at EpochA and none holds at EpochB.
type ReplayGone struct {
	// Entity is a stream or a durable consumer; account_name set when
	// known.
	Entity *v1.EntityRef
	// ServersAtA held it at EpochA; NotAnsweringAtB are those of them
	// that did not answer JSZ at EpochB.
	ServersAtA, NotAnsweringAtB []string
	LastReported                time.Time
	LastEpoch                   int64
	// Delete is its delete event (or its stream's), nil when none was
	// captured.
	Delete *v1.Event
	// DurablesWithIt are a gone stream's durables at A, by name: they went
	// with it.
	DurablesWithIt []string
	// FindingIDs are its findings of ReplayGoneChecks, a stream's also
	// those of its durables; FinishReplay adds those it finds in
	// Transitions.
	FindingIDs []string
}

// Gap kinds.
const (
	GapCollections = "collections"
	GapCollector   = "collector"
	GapKubernetes  = "kubernetes"
)

// ReplayGap is a time without collections (GapCollections), while
// nats-lens was not running (GapCollector, a collector.gap event) or
// while Kubernetes was not watched (GapKubernetes, collector.k8s_gap).
type ReplayGap struct {
	Kind string
	// Start and End bound the gap; End is zero for a gap that had not
	// ended.
	Start, End time.Time
	// BeforeEpoch and AfterEpoch are the collections either side, 0 when
	// none, with their start times.
	BeforeEpoch, AfterEpoch int64
	BeforeAt, AfterAt       time.Time
	// Event is the collector event of the gap; nil for GapCollections.
	Event *v1.Event
}

// ReplayNodes counts Kubernetes node changes.
type ReplayNodes struct {
	Upgraded, Drained, Pools int32
	// PoolLabels is whether an upgraded node carried a node pool label;
	// Unnamed whether a node event between A and B named no node, which
	// leaves the node counts unset, as they cannot be told apart.
	PoolLabels, Unnamed bool
}
