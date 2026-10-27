package store

import (
	"database/sql"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// The writer's earlier JetStream write, one statement per row and a
// lookup per configuration, kept as the reference the JSON batches must
// match.

func (w *writer) writeJetStreamByRow(v *collect.ServerView) {
	if v.Jsz == nil {
		return
	}
	for _, acc := range v.Jsz.AccountDetails {
		if acc == nil {
			continue
		}
		for i := range acc.Streams {
			s := &acc.Streams[i]
			w.writeStreamByRow(v, acc.Id, s)
			for _, c := range s.Consumer {
				if c != nil {
					w.writeConsumerByRow(v, acc.Id, s.Name, c)
				}
			}
		}
	}
}

// clusterCols returns cluster, raft_group, leader, leader_since, is_leader.
func clusterCols(ci *server.ClusterInfo, self string) []any {
	if ci == nil {
		return []any{nil, nil, nil, nil, true}
	}
	return []any{ci.Name, ci.RaftGroup, ci.Leader, tsp(ci.LeaderSince), isLeader(ci, self)}
}

var streamStatsCols = []string{
	"epoch", "server_id", "account", "stream", "created", "messages", "bytes", "first_seq", "last_seq",
	"first_ts", "last_ts", "num_subjects", "num_deleted", "consumer_count",
	"cluster", "raft_group", "leader", "leader_since", "is_leader",
	"replicas_reported", "replicas_current", "replicas_offline",
	"mirror_lag", "mirror_active_ns", "mirror_error", "sources_count", "sources_max_lag",
}

var replicaCols = []string{
	"current", "offline", "active_ns", "lag", "pending",
}

func (w *writer) writeStreamByRow(v *collect.ServerView, account string, s *server.StreamDetail) {
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
		w.configVersionByRow("stream_config",
			[]string{"server_id", "account", "stream"}, []any{v.Info.ID, account, s.Name},
			hash, s.Created, streamConfigCols, []any{
				ts(s.Created), cfg.Description, strs(cfg.Subjects), enum(cfg.Retention), enum(cfg.Storage),
				cfg.Replicas, cfg.MaxConsumers, cfg.MaxMsgs, cfg.MaxBytes, cfg.MaxAge.Nanoseconds(), cfg.MaxMsgsPer,
				int64(cfg.MaxMsgSize), enum(cfg.Discard), cfg.Duplicates.Nanoseconds(), mirror, sources,
				plCluster, plTags, enum(cfg.Compression), cfg.Sealed, cfg.DenyDelete, cfg.DenyPurge,
				cfg.AllowDirect, cfg.AllowMsgTTL, jsonText(cfg),
			})
	}

	st := s.State
	vals := []any{w.epoch, v.Info.ID, account, s.Name, ts(s.Created), i64(st.Msgs), i64(st.Bytes),
		i64(st.FirstSeq), i64(st.LastSeq), ts(st.FirstTime), ts(st.LastTime), int64(st.NumSubjects),
		int64(st.NumDeleted), st.Consumers}
	vals = append(vals, clusterCols(s.Cluster, v.Info.Name)...)
	rep, cur, off := replicaCounts(s.Cluster)
	vals = append(vals, rep, cur, off)
	if m := s.Mirror; m != nil {
		var mErr any
		if m.Error != nil {
			mErr = m.Error.Description
		}
		vals = append(vals, i64(m.Lag), m.Active.Nanoseconds(), mErr)
	} else {
		vals = append(vals, nil, nil, nil)
	}
	// Null sources are not counted, since the fix for null list entries.
	var sources int
	var maxLag uint64
	for _, src := range s.Sources {
		if src != nil {
			sources++
			maxLag = max(maxLag, src.Lag)
		}
	}
	if sources > 0 {
		vals = append(vals, sources, i64(maxLag))
	} else {
		vals = append(vals, nil, nil)
	}
	w.insert("stream_stats", streamStatsCols, vals...)

	if s.Cluster != nil {
		cols := append([]string{"epoch", "server_id", "account", "stream", "peer_name", "peer_id"}, replicaCols...)
		for _, r := range s.Cluster.Replicas {
			if r == nil {
				continue
			}
			w.insert("stream_replicas", cols, w.epoch, v.Info.ID, account, s.Name, r.Name, r.Peer,
				r.Current, r.Offline, r.Active.Nanoseconds(), i64(r.Lag), r.Pending)
		}
	}
}

var consumerStatsCols = []string{
	"epoch", "server_id", "account", "stream", "consumer", "created",
	"delivered_consumer_seq", "delivered_stream_seq", "delivered_last_active",
	"ack_floor_consumer_seq", "ack_floor_stream_seq", "ack_floor_last_active",
	"num_ack_pending", "num_redelivered", "num_waiting", "num_pending", "push_bound", "paused",
	"cluster", "raft_group", "leader", "leader_since", "is_leader",
	"replicas_reported", "replicas_current", "replicas_offline", "reported_at",
}

func (w *writer) writeConsumerByRow(v *collect.ServerView, account, stream string, c *server.ConsumerInfo) {
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
		w.configVersionByRow("consumer_config",
			[]string{"server_id", "account", "stream", "consumer"}, []any{v.Info.ID, account, stream, c.Name},
			hash, c.Created, consumerConfigCols, []any{
				ts(c.Created), cfg.Durable, cfg.Description, enum(cfg.DeliverPolicy), enum(cfg.AckPolicy),
				cfg.AckWait.Nanoseconds(), cfg.MaxDeliver, durs(cfg.BackOff), filterSubjects(cfg),
				enum(cfg.ReplayPolicy), cfg.MaxWaiting, cfg.MaxAckPending, cfg.MaxRequestBatch,
				cfg.DeliverSubject, cfg.DeliverGroup, cfg.Heartbeat.Nanoseconds(), cfg.FlowControl,
				cfg.HeadersOnly, cfg.InactiveThreshold.Nanoseconds(), cfg.Replicas, cfg.MemoryStorage, jsonText(cfg),
			})
	}

	// In nats-server 2.15 a follower reports num_pending and num_waiting as
	// 0 and push_bound as false: the backlog count, pull requests and
	// subscriber interest live on the leader only. Delivered, ack floor,
	// ack pending and redelivered come from replicated state on every replica.
	lead := isLeader(c.Cluster, v.Info.Name)
	vals := []any{w.epoch, v.Info.ID, account, stream, c.Name, ts(c.Created),
		i64(c.Delivered.Consumer), i64(c.Delivered.Stream), tsp(c.Delivered.Last),
		i64(c.AckFloor.Consumer), i64(c.AckFloor.Stream), tsp(c.AckFloor.Last),
		int64(c.NumAckPending), int64(c.NumRedelivered),
		leaderOnly(lead, int64(c.NumWaiting)), leaderOnly(lead, i64(c.NumPending)),
		leaderOnly(lead, c.PushBound), c.Paused}
	vals = append(vals, clusterCols(c.Cluster, v.Info.Name)...)
	rep, cur, off := replicaCounts(c.Cluster)
	vals = append(vals, rep, cur, off, ts(c.TimeStamp))
	w.insert("consumer_stats", consumerStatsCols, vals...)

	if c.Cluster != nil {
		cols := append([]string{"epoch", "server_id", "account", "stream", "consumer", "peer_name", "peer_id"}, replicaCols...)
		for _, r := range c.Cluster.Replicas {
			if r == nil {
				continue
			}
			w.insert("consumer_replicas", cols, w.epoch, v.Info.ID, account, stream, c.Name, r.Name, r.Peer,
				r.Current, r.Offline, r.Active.Nanoseconds(), i64(r.Lag), r.Pending)
		}
	}
}

// configVersionByRow extends the latest configuration row for the key when the
// hash and the creation time are unchanged and the row ends at the previous stored epoch, and
// starts a new row otherwise: when the configuration changed, is new, or the
// server reported nothing for the asset in between (server missing, JSZ
// missing, asset gone and back). A run therefore never covers an epoch in
// which the server did not report the configuration.
func (w *writer) configVersionByRow(table string, keyCols []string, keyVals []any, hash string, created time.Time, cols []string, vals []any) {
	if w.err != nil {
		return
	}
	where := ""
	for i, k := range keyCols {
		if i > 0 {
			where += " AND "
		}
		where += k + " = ?"
	}
	var prevHash string
	var prevFirst, prevLast int64
	var prevCreated sql.NullTime
	err := w.tx.QueryRowContext(w.ctx,
		"SELECT config_hash, first_epoch, last_epoch, created FROM "+table+" WHERE "+where+" ORDER BY first_epoch DESC LIMIT 1",
		keyVals...).Scan(&prevHash, &prevFirst, &prevLast, &prevCreated)
	sameCreated := prevCreated.Valid == !created.IsZero() && (!prevCreated.Valid || prevCreated.Time.Equal(created.Truncate(time.Microsecond)))
	switch {
	case err == nil && prevLast == w.epoch:
		// Already written in this epoch; repeated returns earlier for the
		// writers in this package, this keeps the unique key safe anyway.
		return
	case err == nil && prevHash == hash && sameCreated && prevLast == w.prevEpoch:
		w.exec("UPDATE "+table+" SET last_epoch = ? WHERE "+where+" AND first_epoch = ?",
			append(append([]any{w.epoch}, keyVals...), prevFirst)...)
		return
	case err != nil && !isNoRows(err):
		w.fail(err)
		return
	}
	allCols := append(append(append([]string{}, keyCols...), "first_epoch", "last_epoch", "config_hash"), cols...)
	allVals := append(append(append([]any{}, keyVals...), w.epoch, w.epoch, hash), vals...)
	w.insert(table, allCols, allVals...)
}
