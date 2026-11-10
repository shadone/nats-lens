package api

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Replication kinds.
const (
	ReplicationMirror = "mirror"
	ReplicationSource = "source"
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
	// SourceID is the source's configuration hash, Filter its subject
	// filter; SourceID is empty for a mirror.
	SourceID, Filter string
	// Lag and Active are the leader's report in the epoch, LagBefore and
	// ActiveBefore in the collection before it; nil when not reported. A
	// negative Active means never heard from the origin.
	Lag, LagBefore       *int64
	Active, ActiveBefore *time.Duration
	Error                string
}

// topologyReplication keeps the facts whose two streams lie in different
// known clusters, attaches the finding that fires about each copy, and
// orders them: those with a finding that is not muted or accepted first,
// then by cluster, stream, origin, kind and source.
func topologyReplication(facts []TopologyReplicationFact, firing map[string][]*v1.Finding) []*v1.TopologyReplication {
	var out []*v1.TopologyReplication
	for _, f := range facts {
		if f.Cluster == "" || f.OriginCluster == "" || f.Cluster == f.OriginCluster {
			continue
		}
		r := &v1.TopologyReplication{Account: f.Account, Stream: f.Stream, Cluster: f.Cluster, OriginStream: f.Origin,
			OriginCluster: f.OriginCluster, Kind: f.Kind, Leader: f.Leader, Lag: f.Lag, LagBefore: f.LagBefore, Error: f.Error,
			SourceId: f.SourceID, FilterSubject: f.Filter}
		if f.Active != nil {
			if *f.Active < 0 {
				r.NeverHeard = true
			} else {
				r.LastActive = durationpb.New(*f.Active)
			}
		}
		if x := copyFinding(f, firing); x != nil {
			r.FindingId, r.FindingCode, r.FindingSeverity, r.FindingDetail, r.FindingQuiet = x.GetId(), x.GetCheckCode(), x.GetSeverity(), x.GetDetail(), quiet(x)
			r.FindingAccepted = x.GetMute().GetAccepted()
		}
		out = append(out, r)
	}
	warns := func(r *v1.TopologyReplication) bool { return r.GetFindingId() != "" && !r.GetFindingQuiet() }
	slices.SortFunc(out, func(a, b *v1.TopologyReplication) int {
		return cmp.Or(compareBool(warns(b), warns(a)), cmp.Compare(a.GetCluster(), b.GetCluster()),
			cmp.Compare(a.GetStream(), b.GetStream()), cmp.Compare(a.GetOriginStream(), b.GetOriginStream()),
			cmp.Compare(a.GetKind(), b.GetKind()), cmp.Compare(a.GetSourceId(), b.GetSourceId()), cmp.Compare(a.GetAccount(), b.GetAccount()))
	})
	return out
}

// copyFinding is the finding about one copy: for a mirror stream-017,
// else stream-015; for a source stream-023, else stream-022, when its
// source_ids name this source or it names none.
func copyFinding(f TopologyReplicationFact, firing map[string][]*v1.Finding) *v1.Finding {
	key := f.Account + "/" + f.Stream
	codes := []string{CheckMirrorError, CheckMirrorLagging}
	if f.Kind == ReplicationSource {
		codes = []string{CheckSourceError, CheckSourceSilent}
	}
	for _, code := range codes {
		for _, x := range firing[code+":"+key] {
			if f.Kind == ReplicationMirror || namesSource(x, f.SourceID) {
				return x
			}
		}
	}
	return nil
}

// namesSource reports whether a source finding is about the source id:
// its source_ids list it, or it lists none.
func namesSource(x *v1.Finding, id string) bool {
	for _, c := range x.GetRelatedConfig() {
		if c.GetKey() == "source_ids" {
			return slices.Contains(strings.Split(c.GetValue(), ","), id)
		}
	}
	return true
}
