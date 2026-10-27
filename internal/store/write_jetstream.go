package store

import (
	"fmt"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// writeJetStream stores a server's JSZ streams and consumers: it gathers
// their stats, replica peers and configurations, then writes each table in
// JSON batches (see write_jetstream_batch.go).
func (w *writer) writeJetStream(v *collect.ServerView) {
	if v.Jsz == nil {
		return
	}
	var b jsRows
	for _, acc := range v.Jsz.AccountDetails {
		if acc == nil {
			continue
		}
		for i := range acc.Streams {
			s := &acc.Streams[i]
			w.gatherStream(&b, v, acc.Id, s)
			for _, c := range s.Consumer {
				if c != nil {
					w.gatherConsumer(&b, v, acc.Id, s.Name, c)
				}
			}
		}
	}
	w.flushJetStream(v.Info.ID, &b)
}

// clusterOf is the cluster part of a stream_stats or consumer_stats row.
func clusterOf(ci *server.ClusterInfo, self string) clusterFields {
	if ci == nil {
		return clusterFields{IsLeader: true}
	}
	rep, cur, off := replicaCounts(ci)
	return clusterFields{Cluster: ci.Name, RaftGroup: ci.RaftGroup, Leader: ci.Leader,
		LeaderSince: unixMicroPtr(ci.LeaderSince), IsLeader: isLeader(ci, self),
		ReplicasReported: rep, ReplicasCurrent: cur, ReplicasOffline: off}
}

// replicasOf is the replica rows of the peers a server lists for an asset.
func replicasOf(ci *server.ClusterInfo, account, stream, consumer string) []replicaRow {
	if ci == nil {
		return nil
	}
	out := make([]replicaRow, 0, len(ci.Replicas))
	for _, r := range ci.Replicas {
		if r == nil {
			continue
		}
		out = append(out, replicaRow{Account: account, Stream: stream, Consumer: consumer, PeerName: r.Name,
			PeerID: r.Peer, Current: r.Current, Offline: r.Offline, ActiveNS: r.Active.Nanoseconds(),
			Lag: i64(r.Lag), Pending: r.Pending})
	}
	return out
}

func (w *writer) gatherStream(b *jsRows, v *collect.ServerView, account string, s *server.StreamDetail) {
	key := assetKey{server: v.Info.ID, account: account, stream: s.Name}
	hash, hashErr := configHash(s.Config)
	if w.repeated(v, key, repeatHash(hash, hashErr)+" "+s.Created.UTC().String()) {
		return
	}
	w.ids.stream(account, s.Name, s.Created)
	if s.Cluster != nil && s.Cluster.RaftGroup != "" {
		w.ids.raftAsset(s.Cluster.RaftGroup, account, s.Name, "")
	}
	if cfg := s.Config; cfg != nil && hashErr != nil {
		w.unhashed(v, key, hashErr)
	} else if cfg != nil {
		var mirror any
		if cfg.Mirror != nil {
			mirror = cfg.Mirror.Name
		}
		sources := []string{}
		for _, src := range cfg.Sources {
			if src != nil {
				sources = append(sources, src.Name)
			}
		}
		var plCluster any
		plTags := []string{}
		if cfg.Placement != nil {
			plCluster, plTags = cfg.Placement.Cluster, strs(cfg.Placement.Tags)
		}
		b.streamConfigs = append(b.streamConfigs, streamConfigRow{
			Account: account, Stream: s.Name, ConfigHash: hash, Created: unixMicro(s.Created),
			Description: cfg.Description, Subjects: strs(cfg.Subjects), Retention: enum(cfg.Retention),
			Storage: enum(cfg.Storage), Replicas: int64(cfg.Replicas), MaxConsumers: int64(cfg.MaxConsumers),
			MaxMsgs: cfg.MaxMsgs, MaxBytes: cfg.MaxBytes, MaxAgeNS: cfg.MaxAge.Nanoseconds(),
			MaxMsgsPerSubject: cfg.MaxMsgsPer, MaxMsgSize: int64(cfg.MaxMsgSize), Discard: enum(cfg.Discard),
			DuplicateWindowNS: cfg.Duplicates.Nanoseconds(), Mirror: mirror, Sources: sources,
			PlacementCluster: plCluster, PlacementTags: plTags, Compression: enum(cfg.Compression),
			Sealed: cfg.Sealed, DenyDelete: cfg.DenyDelete, DenyPurge: cfg.DenyPurge,
			AllowDirect: cfg.AllowDirect, AllowMsgTTL: cfg.AllowMsgTTL, Config: jsonText(cfg),
		})
	}

	st := s.State
	row := streamStatsRow{Account: account, Stream: s.Name, Created: unixMicro(s.Created),
		Messages: i64(st.Msgs), Bytes: i64(st.Bytes), FirstSeq: i64(st.FirstSeq), LastSeq: i64(st.LastSeq),
		FirstTS: unixMicro(st.FirstTime), LastTS: unixMicro(st.LastTime), NumSubjects: int64(st.NumSubjects),
		NumDeleted: int64(st.NumDeleted), ConsumerCount: int64(st.Consumers),
		clusterFields: clusterOf(s.Cluster, v.Info.Name)}
	if m := s.Mirror; m != nil {
		row.MirrorLag, row.MirrorActiveNS = i64(m.Lag), m.Active.Nanoseconds()
		if m.Error != nil {
			row.MirrorError = m.Error.Description
		}
	}
	var sources int
	var maxLag uint64
	for _, src := range s.Sources {
		if src != nil {
			sources++
			maxLag = max(maxLag, src.Lag)
		}
	}
	if sources > 0 {
		row.SourcesCount, row.SourcesMaxLag = sources, i64(maxLag)
	}
	b.streams = append(b.streams, row)
	b.streamReplicas = append(b.streamReplicas, replicasOf(s.Cluster, account, s.Name, "")...)
}

func (w *writer) gatherConsumer(b *jsRows, v *collect.ServerView, account, stream string, c *server.ConsumerInfo) {
	key := assetKey{server: v.Info.ID, account: account, stream: stream, consumer: c.Name}
	hash, hashErr := configHash(c.Config)
	if w.repeated(v, key, repeatHash(hash, hashErr)+" "+c.Created.UTC().String()) {
		return
	}
	w.ids.consumer(account, stream, c.Name, c.Created)
	if c.Cluster != nil && c.Cluster.RaftGroup != "" {
		w.ids.raftAsset(c.Cluster.RaftGroup, account, stream, c.Name)
	}
	if cfg := c.Config; cfg != nil && hashErr != nil {
		w.unhashed(v, key, hashErr)
	} else if cfg != nil {
		b.consumerConfigs = append(b.consumerConfigs, consumerConfigRow{
			Account: account, Stream: stream, Consumer: c.Name, ConfigHash: hash,
			Created: unixMicro(c.Created), DurableName: cfg.Durable, Description: cfg.Description,
			DeliverPolicy: enum(cfg.DeliverPolicy), AckPolicy: enum(cfg.AckPolicy),
			AckWaitNS: cfg.AckWait.Nanoseconds(), MaxDeliver: int64(cfg.MaxDeliver), BackoffNS: durs(cfg.BackOff),
			FilterSubjects: filterSubjects(cfg), ReplayPolicy: enum(cfg.ReplayPolicy),
			MaxWaiting: int64(cfg.MaxWaiting), MaxAckPending: int64(cfg.MaxAckPending),
			MaxBatch: int64(cfg.MaxRequestBatch), DeliverSubject: cfg.DeliverSubject,
			DeliverGroup: cfg.DeliverGroup, IdleHeartbeatNS: cfg.Heartbeat.Nanoseconds(),
			FlowControl: cfg.FlowControl, HeadersOnly: cfg.HeadersOnly,
			InactiveThresholdNS: cfg.InactiveThreshold.Nanoseconds(), Replicas: int64(cfg.Replicas),
			MemStorage: cfg.MemoryStorage, Config: jsonText(cfg),
		})
	}

	// In nats-server 2.15 a follower reports num_pending and num_waiting as
	// 0 and push_bound as false: the backlog count, pull requests and
	// subscriber interest live on the leader only. Delivered, ack floor,
	// ack pending and redelivered come from replicated state on every replica.
	lead := isLeader(c.Cluster, v.Info.Name)
	b.consumers = append(b.consumers, consumerStatsRow{
		Account: account, Stream: stream, Consumer: c.Name, Created: unixMicro(c.Created),
		DeliveredConsumerSeq: i64(c.Delivered.Consumer), DeliveredStreamSeq: i64(c.Delivered.Stream),
		DeliveredLastActive: unixMicroPtr(c.Delivered.Last),
		AckFloorConsumerSeq: i64(c.AckFloor.Consumer), AckFloorStreamSeq: i64(c.AckFloor.Stream),
		AckFloorLastActive: unixMicroPtr(c.AckFloor.Last),
		NumAckPending:      int64(c.NumAckPending), NumRedelivered: int64(c.NumRedelivered),
		NumWaiting: leaderOnly(lead, int64(c.NumWaiting)), NumPending: leaderOnly(lead, i64(c.NumPending)),
		PushBound: leaderOnly(lead, c.PushBound), Paused: c.Paused,
		clusterFields: clusterOf(c.Cluster, v.Info.Name), ReportedAt: unixMicro(c.TimeStamp),
	})
	b.consumerReplicas = append(b.consumerReplicas, replicasOf(c.Cluster, account, stream, c.Name)...)
}

// replicaCounts summarises the peers a server lists for an asset; a null
// entry is skipped, as it is for the replica rows.
func replicaCounts(ci *server.ClusterInfo) (reported, current, offline any) {
	if ci == nil {
		return nil, nil, nil
	}
	n, c, o := 0, 0, 0
	for _, r := range ci.Replicas {
		if r == nil {
			continue
		}
		n++
		if r.Current {
			c++
		}
		if r.Offline {
			o++
		}
	}
	return n, c, o
}

// isLeader reports whether the reporting server leads the asset. Without
// cluster info (JetStream not clustered) the server holds the only copy.
func isLeader(ci *server.ClusterInfo, self string) bool {
	return ci == nil || (ci.Leader != "" && ci.Leader == self)
}

// leaderOnly returns v on the leader's row and NULL on a follower's, for
// values nats-server tracks only on the leader.
func leaderOnly(leader bool, v any) any {
	if !leader {
		return nil
	}
	return v
}

var streamConfigCols = []string{
	"created", "description", "subjects", "retention", "storage", "replicas", "max_consumers",
	"max_msgs", "max_bytes", "max_age_ns", "max_msgs_per_subject", "max_msg_size", "discard",
	"duplicate_window_ns", "mirror", "sources", "placement_cluster", "placement_tags", "compression",
	"sealed", "deny_delete", "deny_purge", "allow_direct", "allow_msg_ttl", "config",
}
var consumerConfigCols = []string{
	"created", "durable_name", "description", "deliver_policy", "ack_policy", "ack_wait_ns", "max_deliver",
	"backoff_ns", "filter_subjects", "replay_policy", "max_waiting", "max_ack_pending", "max_batch",
	"deliver_subject", "deliver_group", "idle_heartbeat_ns", "flow_control", "headers_only",
	"inactive_threshold_ns", "replicas", "mem_storage", "config",
}

func filterSubjects(cfg *server.ConsumerConfig) []string {
	out := []string{}
	if cfg.FilterSubject != "" {
		out = append(out, cfg.FilterSubject)
	}
	return append(out, cfg.FilterSubjects...)
}

// assetKey identifies one server's report of a stream (consumer empty) or
// consumer within an epoch.
type assetKey struct {
	server, account, stream, consumer string
}

func (k assetKey) String() string {
	if k.consumer == "" {
		return fmt.Sprintf("stream %q in account %q", k.stream, k.account)
	}
	return fmt.Sprintf("consumer %q on stream %q in account %q", k.consumer, k.stream, k.account)
}

// repeated reports whether this server already reported the asset in this
// epoch, as when JSZ lists an account or stream twice. The first report is
// kept and later ones are skipped. A repeat with a different configuration
// is noted in the epoch's errors, once per epoch, and logged once per
// process.
func (w *writer) repeated(v *collect.ServerView, key assetKey, hash string) bool {
	first, ok := w.seen[key]
	if !ok {
		w.seen[key] = hash
		return false
	}
	if first != hash && !w.noted[key] {
		w.noted[key] = true
		w.notes = append(w.notes, fmt.Sprintf("%s reported %s twice with different configuration; kept the first", v.Info.Name, key))
		if !w.warned[key] {
			w.warned[key] = true
			w.log.Warn("conflicting duplicate in JSZ; logged once per process", "server", v.Info.Name, "asset", key.String())
		}
	}
	return true
}
