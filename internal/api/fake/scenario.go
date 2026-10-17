package fake

import (
	"time"

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
	// epoch is the number of the newest epoch; stale is how long before
	// now the data ends (zero while collecting).
	epoch int64
	stale time.Duration
	// partial is whether the newest epoch missed a server.
	partial bool
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
	events  func(s scen, fs []*v1.Finding) []*v1.Event
	rows    func(sc *scenario) []rowSpec
	windows func(s scen, r *v1.TimeRange) []*v1.TimeWindow
	// k8sLanes draws the Kubernetes lane through lane.
	k8sLanes func(s scen, lane func(at time.Time, kind, short, label string))
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
