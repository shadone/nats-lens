package fake

import (
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// scenario is the data of one demo system. Every time in it is in the
// fixed frame (FixedNow is 2026-10-17T14:30:00Z); scen moves it to the
// provider's present. The functions build the parts that differ between
// systems; the RPCs around them are shared.
type scenario struct {
	// cluster is the cluster name Info and GetOverview report.
	cluster string
	// servers, streams and clients are the system's servers, its streams
	// in account Account, and the client names of its connections.
	servers []serverSpec
	streams []streamSpec
	clients []string
	// buckets are the KV buckets and object stores the inventory lists;
	// a KV_ or OBJ_ stream without one is a bucket with defaults.
	buckets []bucketSpec
	// metaLeader is the JetStream meta leader.
	metaLeader string
	// stale is how long before now the data ends (zero while
	// collecting).
	stale time.Duration
	// history is how long before now the stored history begins; zero
	// means defaultHistory, older than any range.
	history time.Duration
	// serverDown reports whether a server was down at fixed-frame time
	// c, as its series draw it, so that an epoch then missed it and lists
	// none of its connections; nil means never.
	serverDown func(sp serverSpec, c time.Time) bool
	// collection, collectionError and errorSince describe the collector;
	// errorSince is a time in the fixed frame.
	collection      v1.CollectionState
	collectionError string
	errorSince      time.Time
	// routineClient names the client whose routine reconnects fill the
	// events; they stay off the timeline's NATS lane.
	routineClient string

	findings func(s scen) []*v1.Finding
	// events are the events newest first, without IDs; fs are the
	// findings with the mutes applied.
	events func(s scen, fs []*v1.Finding) []*v1.Event
	rows   func(sc *scenario) []rowSpec
	// infoUntil holds, by check code, when a row's findings changed from
	// info to its severity, in the fixed frame: its cells are info before.
	infoUntil map[string]time.Time
	// kubernetes is whether the system watches Kubernetes: its
	// Kubernetes events draw the lane and the windows, by the store's
	// rules.
	kubernetes bool
	// sample is a metric's value at fixed-frame time c; ok is false where
	// there is no data.
	sample func(sc *scenario, metric string, e *v1.EntityRef, c time.Time) (float64, bool)
	// slowEvents are the times of the slow consumer disconnects.
	slowEvents []time.Time
	// mutes are the mutes the system starts with.
	mutes func() []*storedMute
	// transitions, when set, returns a finding's transitions newest
	// first; nil means a firing and, once resolved, a resolve.
	transitions func(s scen, f *v1.Finding) []*v1.FindingTransition
	// consumer adjusts a consumer's summary; may be nil.
	consumer func(s scen, c *v1.ConsumerSummary)
	// created returns each peer's creation time of a consumer, in peer
	// order, and whether they disagree; nil means the same time on all.
	created func(s scen, stream, consumer string) ([]time.Time, bool)
	// gone returns the assets servers held at Replay's A and none holds
	// at its B besides the vanished durables of the streams; nil means
	// none.
	gone func(s scen, f *api.ReplayFacts) []api.ReplayGone
	// initialLeaders are the leaders, by entity key, that differ at the
	// start of the history from a stream's first peer and a consumer's
	// stream leader.
	initialLeaders map[string]string
	// spreadLeaders spreads the initial stream leaders over the replicas:
	// the i-th stream's is its peer i modulo its replicas, not its first.
	spreadLeaders bool
	// auth is what the Accounts and auth page reads; nil means the stream
	// account alone, never looked up, with no failed logins, certificates
	// or user JWTs.
	auth *authSpec
	// links are how the servers connect; nil means routes only, one per
	// pair of servers.
	links *linkSpec
	// subs are the subscriptions each client's connections hold, by client
	// name; nil means the scenario collects no subscription lists.
	subs map[string][]clientSub
	// extraConns are connections besides the clients' in Account.
	extraConns []extraConn
	// library, when set, gives the client library of a client's
	// connections on a server; an empty language falls back to libraryOf's
	// default.
	library func(sp serverSpec, client string) (lang, version string)
	// micro are the micro services the clients run in Account, and
	// microAsked the accounts nats-lens asks with $SRV requests: Account
	// answers, any other is refused.
	micro      []microSpec
	microAsked []string
	// microTimeouts are the epochs, in the fixed frame, whose $SRV
	// requests to Account timed out.
	microTimeouts []time.Time
	// apiRate and apiErrors are the stream account's JetStream API
	// requests per second and the share that failed.
	apiRate, apiErrors float64
	// layout indexes the servers by cluster, built once.
	layoutOnce sync.Once
	layout     clusterLayout
	// model is the leaders' history, built once from the scenario.
	modelOnce sync.Once
	model     *leaderModel
	// ids are the events' IDs by kind and time, counted once.
	idsOnce sync.Once
	ids     map[string]string
}

// systemRef is the scenario's system entity.
func (sc *scenario) systemRef() *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM, Name: sc.cluster}
}

// peers are the servers that hold a stream's replicas: the stream's own
// list, else the first three servers.
func (sc *scenario) peers(sp streamSpec) []string {
	if len(sp.peers) > 0 {
		return sp.peers
	}
	var out []string
	for _, sv := range sc.servers[:min(3, len(sc.servers))] {
		out = append(out, sv.name)
	}
	return out
}

// server returns the spec of a server by name.
func (sc *scenario) server(name string) (serverSpec, bool) {
	for _, sp := range sc.servers {
		if sp.name == name {
			return sp, true
		}
	}
	return serverSpec{}, false
}

// clusterSize is the number of servers in a cluster.
func (sc *scenario) clusterSize(cluster string) int {
	n := 0
	for _, sp := range sc.servers {
		if sp.cluster == cluster {
			n++
		}
	}
	return n
}
