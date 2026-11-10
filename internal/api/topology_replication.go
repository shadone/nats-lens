package api

import (
	"cmp"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Replication kinds.
const (
	ReplicationMirror = "mirror"
	ReplicationSource = "source"
)

// The replication warnings: a copy is not active after
// ReplicationMaxInactive without hearing from its origin, the default
// max_inactive of stream-015 and stream-022, and falls behind when its lag
// is at least ReplicationBehindLag and grew since the collection before.
const (
	ReplicationMaxInactive = 2 * time.Minute
	ReplicationBehindLag   = 1000
)

// TopologyReplicationFact is one mirror or source of a stream of the epoch
// whose origin, in the same account and not external, a server reports.
type TopologyReplicationFact struct {
	Account, Stream, Cluster string
	// Kind is ReplicationMirror or ReplicationSource.
	Kind                  string
	Origin, OriginCluster string
	// Leader leads the copying stream; empty without a leader, and then
	// the runtime fields are nil.
	Leader string
	// Lag and Active are the leader's report in the epoch, LagBefore and
	// ActiveBefore in the collection before it; nil when not reported. A
	// negative Active means never heard from the origin.
	Lag, LagBefore       *int64
	Active, ActiveBefore *time.Duration
	Error                string
}

// topologyReplication keeps the facts whose two streams lie in different
// known clusters, judges them, and orders them: warnings first, then by
// cluster, stream, origin and kind.
func topologyReplication(facts []TopologyReplicationFact) []*v1.TopologyReplication {
	var out []*v1.TopologyReplication
	for _, f := range facts {
		if f.Cluster == "" || f.OriginCluster == "" || f.Cluster == f.OriginCluster {
			continue
		}
		r := &v1.TopologyReplication{Account: f.Account, Stream: f.Stream, Cluster: f.Cluster, OriginStream: f.Origin,
			OriginCluster: f.OriginCluster, Kind: f.Kind, Leader: f.Leader, Lag: f.Lag, LagBefore: f.LagBefore, Error: f.Error}
		if f.Active != nil {
			if *f.Active < 0 {
				r.NeverHeard = true
			} else {
				r.LastActive = durationpb.New(*f.Active)
			}
		}
		r.NotActive = f.Active != nil && (*f.Active > ReplicationMaxInactive ||
			*f.Active < 0 && f.ActiveBefore != nil && *f.ActiveBefore < 0)
		r.FallingBehind = f.Lag != nil && f.LagBefore != nil && *f.Lag >= ReplicationBehindLag && *f.Lag > *f.LagBefore
		out = append(out, r)
	}
	warns := func(r *v1.TopologyReplication) bool {
		return r.GetError() != "" || r.GetNotActive() || r.GetFallingBehind()
	}
	slices.SortFunc(out, func(a, b *v1.TopologyReplication) int {
		return cmp.Or(compareBool(warns(b), warns(a)), cmp.Compare(a.GetCluster(), b.GetCluster()),
			cmp.Compare(a.GetStream(), b.GetStream()), cmp.Compare(a.GetOriginStream(), b.GetOriginStream()),
			cmp.Compare(a.GetKind(), b.GetKind()), cmp.Compare(a.GetAccount(), b.GetAccount()))
	})
	return out
}
