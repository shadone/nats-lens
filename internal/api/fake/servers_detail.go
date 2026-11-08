package fake

import (
	"fmt"
	"hash/fnv"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// defaultMaxPayload is the max_payload of a server whose spec sets none.
const defaultMaxPayload = 1 << 20

// slowTimes are the fixed-frame times of the slow consumer disconnects on
// sp: its own when the scenario places them, else every n-th of the
// scenario's, by the server's position.
func (sc *scenario) slowTimes(sp serverSpec) []time.Time {
	for _, x := range sc.servers {
		if x.slow != nil {
			return sp.slow
		}
	}
	var out []time.Time
	for i, x := range sc.servers {
		if x.name != sp.name {
			continue
		}
		for k, t := range sc.slowEvents {
			if k%len(sc.servers) == i {
				out = append(out, t)
			}
		}
	}
	return out
}

// slowCount counts sp's slow consumer disconnects from after up to before
// the scenario's data end.
func (s scen) slowCount(sp serverSpec, after, before time.Time) int64 {
	before = earliest(before, s.dataEnd())
	var n int64
	for _, t := range s.sc.slowTimes(sp) {
		if c := slowReportAt(t.Add(s.shift)); !c.Before(after) && c.Before(before) {
			n++
		}
	}
	return n
}

// leads counts the streams sp leads at the scenario time at and the
// consumers it leads; ephemeral ones follow their stream's leader.
func (s scen) leads(sp serverSpec, at time.Time) (streams, consumers int32) {
	for _, st := range s.sc.streams {
		if s.streamLeaderAt(st, at) == sp.name {
			streams++
			consumers += int32(st.ephemeral)
		}
		for _, n := range s.consumersAt(st, at) {
			if s.consumerLeaderAt(st, n, at) == sp.name {
				consumers++
			}
		}
	}
	return streams, consumers
}

// replicas counts the streams with a replica on sp and their consumers.
func (s scen) replicas(sp serverSpec) (streams, consumers int32) {
	for _, st := range s.sc.streams {
		for _, peer := range s.sc.peers(st) {
			if peer == sp.name {
				streams++
				consumers += int32(len(st.consumers) + st.ephemeral)
			}
		}
	}
	return streams, consumers
}

func payloadOf(sp serverSpec) int64 {
	if sp.maxPayload == 0 {
		return defaultMaxPayload
	}
	return sp.maxPayload
}

func (sc *scenario) limits(sp serverSpec) *v1.ServerLimits {
	return &v1.ServerLimits{
		MaxPayload: ptr(payloadOf(sp)), MaxPending: ptr(int64(64 * mib)), WriteDeadline: dur(10 * time.Second),
		PingInterval: dur(2 * time.Minute), MaxPingsOut: ptr(int32(2)), JsMaxStorage: ptr(sp.maxStore),
		JsMaxMemory: ptr(int64(2 * gib)), MaxConnections: ptr(int32(65536)),
	}
}

// limitsDiffer names sp's limits that differ from the plurality's, by the
// store provider's rule; in the demo only max_payload varies.
func (sc *scenario) limitsDiffer(sp serverSpec) []string {
	values := make([]*int64, len(sc.servers))
	for i, x := range sc.servers {
		values[i] = ptr(payloadOf(x))
	}
	if api.OffPlurality(ptr(payloadOf(sp)), values) {
		return []string{"max_payload"}
	}
	return nil
}

// routeViews are sp's routes at the scenario time at: the pooled and
// pinned routes to every other server of its cluster that answered then,
// with the round trips the Topology page reads.
func (s scen) routeViews(sp serverSpec, at time.Time) []*v1.RouteView {
	sc := s.sc
	var out []*v1.RouteView
	for _, x := range sc.servers {
		if x.cluster != sp.cluster || x.name == sp.name || s.downAt(x, at) {
			continue
		}
		for range sc.routeAccounts() {
			out = append(out, &v1.RouteView{RemoteName: x.name, Rtt: dur(sc.routeRTT(sp.name, x.name)), PendingBytes: ptr(int64(0))})
		}
	}
	return out
}

// gateways counts the other clusters of the scenario.
func (sc *scenario) gateways(sp serverSpec) int32 {
	seen := map[string]bool{}
	for _, x := range sc.servers {
		if x.cluster != sp.cluster {
			seen[x.cluster] = true
		}
	}
	return int32(len(seen))
}

// kubernetes is where sp's pod runs; nil for a scenario without the
// watcher, which draws no Kubernetes lane.
func (s scen) kubernetes(sp serverSpec) *v1.KubernetesPlacement {
	if !s.sc.kubernetes {
		return nil
	}
	k := &v1.KubernetesPlacement{Pod: sp.name, Namespace: "nats"}
	if sp.podPending {
		return k
	}
	k.Node = sp.node
	if k.Node == "" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(sp.name))
		k.Node = fmt.Sprintf("gke-example-pool-1-%04x", h.Sum32()&0xffff)
	}
	k.ScheduledAt = ts(s.started(sp).Add(-40 * time.Second))
	return k
}

// detail fills the parts of GetServer beyond the summary.
func (s scen) detail(sp serverSpec, res *v1.GetServerResponse, r *v1.TimeRange) {
	at := s.answeredAt(sp, r)
	res.RouteViews = s.routeViews(sp, at)
	res.Routes = ptr(int32(len(res.RouteViews)))
	res.Gateways, res.Leafnodes = ptr(s.sc.gateways(sp)), ptr(s.leafCount(sp, at))
	res.Limits, res.LimitsDiffer = s.sc.limits(sp), s.sc.limitsDiffer(sp)
	res.Kubernetes = s.kubernetes(sp)
	res.ConfigLoadedAt = ts(s.started(sp))
	res.SlowConsumers = ptr(s.slowCount(sp, s.started(sp), s.now))
	streams, consumers := s.replicas(sp)
	res.JsStreams, res.JsConsumers = ptr(streams), ptr(consumers)
}

// answeredAt is the time of the range's epoch, or, for a server that
// stopped answering before it, of its last epoch, as the store reads a
// server's newest rows.
func (s scen) answeredAt(sp serverSpec, r *v1.TimeRange) time.Time {
	at := s.epochTime(r)
	if s.downAt(sp, at) && !sp.down.IsZero() {
		if t, _, ok := s.epochBefore(&v1.TimeRange{Start: ts(s.dataStart()), End: ts(sp.down.Add(s.shift))}); ok {
			at = t
		}
	}
	return at
}
