package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// jsRows is one server's JSZ rows, gathered by writeJetStream and written
// by flushJetStream. Every JSON field is named after its column. Times are
// Unix microseconds of UTC, made into TIMESTAMP in SQL so that no session
// time zone shifts them; nil fields are NULL.
type jsRows struct {
	streams          []streamStatsRow
	consumers        []consumerStatsRow
	streamReplicas   []replicaRow
	consumerReplicas []replicaRow
	streamConfigs    []streamConfigRow
	consumerConfigs  []consumerConfigRow
}

// clusterFields are the cluster columns of stream_stats and consumer_stats.
type clusterFields struct {
	Cluster          any  `json:"cluster"`
	RaftGroup        any  `json:"raft_group"`
	Leader           any  `json:"leader"`
	LeaderSince      any  `json:"leader_since"`
	IsLeader         bool `json:"is_leader"`
	ReplicasReported any  `json:"replicas_reported"`
	ReplicasCurrent  any  `json:"replicas_current"`
	ReplicasOffline  any  `json:"replicas_offline"`
}

const clusterShape = `"cluster":"VARCHAR","raft_group":"VARCHAR","leader":"VARCHAR","leader_since":"BIGINT",` +
	`"is_leader":"BOOLEAN","replicas_reported":"BIGINT","replicas_current":"BIGINT","replicas_offline":"BIGINT"`

type streamStatsRow struct {
	Account       string `json:"account"`
	Stream        string `json:"stream"`
	Created       any    `json:"created"`
	Messages      int64  `json:"messages"`
	Bytes         int64  `json:"bytes"`
	FirstSeq      int64  `json:"first_seq"`
	LastSeq       int64  `json:"last_seq"`
	FirstTS       any    `json:"first_ts"`
	LastTS        any    `json:"last_ts"`
	NumSubjects   int64  `json:"num_subjects"`
	NumDeleted    int64  `json:"num_deleted"`
	ConsumerCount int64  `json:"consumer_count"`
	clusterFields
	MirrorLag      any `json:"mirror_lag"`
	MirrorActiveNS any `json:"mirror_active_ns"`
	MirrorError    any `json:"mirror_error"`
	SourcesCount   any `json:"sources_count"`
	SourcesMaxLag  any `json:"sources_max_lag"`
}

const streamStatsShape = `[{"account":"VARCHAR","stream":"VARCHAR","created":"BIGINT","messages":"BIGINT",` +
	`"bytes":"BIGINT","first_seq":"BIGINT","last_seq":"BIGINT","first_ts":"BIGINT","last_ts":"BIGINT",` +
	`"num_subjects":"BIGINT","num_deleted":"BIGINT","consumer_count":"BIGINT",` + clusterShape + `,` +
	`"mirror_lag":"BIGINT","mirror_active_ns":"BIGINT","mirror_error":"VARCHAR","sources_count":"BIGINT",` +
	`"sources_max_lag":"BIGINT"}]`

type consumerStatsRow struct {
	Account              string `json:"account"`
	Stream               string `json:"stream"`
	Consumer             string `json:"consumer"`
	Created              any    `json:"created"`
	DeliveredConsumerSeq int64  `json:"delivered_consumer_seq"`
	DeliveredStreamSeq   int64  `json:"delivered_stream_seq"`
	DeliveredLastActive  any    `json:"delivered_last_active"`
	AckFloorConsumerSeq  int64  `json:"ack_floor_consumer_seq"`
	AckFloorStreamSeq    int64  `json:"ack_floor_stream_seq"`
	AckFloorLastActive   any    `json:"ack_floor_last_active"`
	NumAckPending        int64  `json:"num_ack_pending"`
	NumRedelivered       int64  `json:"num_redelivered"`
	NumWaiting           any    `json:"num_waiting"`
	NumPending           any    `json:"num_pending"`
	PushBound            any    `json:"push_bound"`
	Paused               bool   `json:"paused"`
	clusterFields
	ReportedAt any `json:"reported_at"`
}

const consumerStatsShape = `[{"account":"VARCHAR","stream":"VARCHAR","consumer":"VARCHAR","created":"BIGINT",` +
	`"delivered_consumer_seq":"BIGINT","delivered_stream_seq":"BIGINT","delivered_last_active":"BIGINT",` +
	`"ack_floor_consumer_seq":"BIGINT","ack_floor_stream_seq":"BIGINT","ack_floor_last_active":"BIGINT",` +
	`"num_ack_pending":"BIGINT","num_redelivered":"BIGINT","num_waiting":"BIGINT","num_pending":"BIGINT",` +
	`"push_bound":"BOOLEAN","paused":"BOOLEAN",` + clusterShape + `,"reported_at":"BIGINT"}]`

// replicaRow is a row of stream_replicas (Consumer empty and left out) or
// consumer_replicas.
type replicaRow struct {
	Account  string `json:"account"`
	Stream   string `json:"stream"`
	Consumer string `json:"consumer,omitempty"`
	PeerName string `json:"peer_name"`
	PeerID   string `json:"peer_id"`
	Current  bool   `json:"current"`
	Offline  bool   `json:"offline"`
	ActiveNS int64  `json:"active_ns"`
	Lag      int64  `json:"lag"`
	Pending  bool   `json:"pending"`
}

const replicaShape = `"peer_name":"VARCHAR","peer_id":"VARCHAR","current":"BOOLEAN","offline":"BOOLEAN",` +
	`"active_ns":"BIGINT","lag":"BIGINT","pending":"BOOLEAN"}]`

type streamConfigRow struct {
	Account           string   `json:"account"`
	Stream            string   `json:"stream"`
	ConfigHash        string   `json:"config_hash"`
	Created           any      `json:"created"`
	Description       string   `json:"description"`
	Subjects          []string `json:"subjects"`
	Retention         string   `json:"retention"`
	Storage           string   `json:"storage"`
	Replicas          int64    `json:"replicas"`
	MaxConsumers      int64    `json:"max_consumers"`
	MaxMsgs           int64    `json:"max_msgs"`
	MaxBytes          int64    `json:"max_bytes"`
	MaxAgeNS          int64    `json:"max_age_ns"`
	MaxMsgsPerSubject int64    `json:"max_msgs_per_subject"`
	MaxMsgSize        int64    `json:"max_msg_size"`
	Discard           string   `json:"discard"`
	DuplicateWindowNS int64    `json:"duplicate_window_ns"`
	Mirror            any      `json:"mirror"`
	Sources           []string `json:"sources"`
	PlacementCluster  any      `json:"placement_cluster"`
	PlacementTags     []string `json:"placement_tags"`
	Compression       string   `json:"compression"`
	Sealed            bool     `json:"sealed"`
	DenyDelete        bool     `json:"deny_delete"`
	DenyPurge         bool     `json:"deny_purge"`
	AllowDirect       bool     `json:"allow_direct"`
	AllowMsgTTL       bool     `json:"allow_msg_ttl"`
	Config            any      `json:"config"`
}

const streamConfigShape = `[{"account":"VARCHAR","stream":"VARCHAR","config_hash":"VARCHAR","created":"BIGINT",` +
	`"description":"VARCHAR","subjects":["VARCHAR"],"retention":"VARCHAR","storage":"VARCHAR",` +
	`"replicas":"BIGINT","max_consumers":"BIGINT","max_msgs":"BIGINT","max_bytes":"BIGINT",` +
	`"max_age_ns":"BIGINT","max_msgs_per_subject":"BIGINT","max_msg_size":"BIGINT","discard":"VARCHAR",` +
	`"duplicate_window_ns":"BIGINT","mirror":"VARCHAR","sources":["VARCHAR"],"placement_cluster":"VARCHAR",` +
	`"placement_tags":["VARCHAR"],"compression":"VARCHAR","sealed":"BOOLEAN","deny_delete":"BOOLEAN",` +
	`"deny_purge":"BOOLEAN","allow_direct":"BOOLEAN","allow_msg_ttl":"BOOLEAN","config":"VARCHAR"}]`

type consumerConfigRow struct {
	Account             string   `json:"account"`
	Stream              string   `json:"stream"`
	Consumer            string   `json:"consumer"`
	ConfigHash          string   `json:"config_hash"`
	Created             any      `json:"created"`
	DurableName         string   `json:"durable_name"`
	Description         string   `json:"description"`
	DeliverPolicy       string   `json:"deliver_policy"`
	AckPolicy           string   `json:"ack_policy"`
	AckWaitNS           int64    `json:"ack_wait_ns"`
	MaxDeliver          int64    `json:"max_deliver"`
	BackoffNS           []int64  `json:"backoff_ns"`
	FilterSubjects      []string `json:"filter_subjects"`
	ReplayPolicy        string   `json:"replay_policy"`
	MaxWaiting          int64    `json:"max_waiting"`
	MaxAckPending       int64    `json:"max_ack_pending"`
	MaxBatch            int64    `json:"max_batch"`
	DeliverSubject      string   `json:"deliver_subject"`
	DeliverGroup        string   `json:"deliver_group"`
	IdleHeartbeatNS     int64    `json:"idle_heartbeat_ns"`
	FlowControl         bool     `json:"flow_control"`
	HeadersOnly         bool     `json:"headers_only"`
	InactiveThresholdNS int64    `json:"inactive_threshold_ns"`
	Replicas            int64    `json:"replicas"`
	MemStorage          bool     `json:"mem_storage"`
	Config              any      `json:"config"`
}

const consumerConfigShape = `[{"account":"VARCHAR","stream":"VARCHAR","consumer":"VARCHAR","config_hash":"VARCHAR",` +
	`"created":"BIGINT","durable_name":"VARCHAR","description":"VARCHAR","deliver_policy":"VARCHAR",` +
	`"ack_policy":"VARCHAR","ack_wait_ns":"BIGINT","max_deliver":"BIGINT","backoff_ns":["BIGINT"],` +
	`"filter_subjects":["VARCHAR"],"replay_policy":"VARCHAR","max_waiting":"BIGINT","max_ack_pending":"BIGINT",` +
	`"max_batch":"BIGINT","deliver_subject":"VARCHAR","deliver_group":"VARCHAR","idle_heartbeat_ns":"BIGINT",` +
	`"flow_control":"BOOLEAN","headers_only":"BOOLEAN","inactive_threshold_ns":"BIGINT","replicas":"BIGINT",` +
	`"mem_storage":"BOOLEAN","config":"VARCHAR"}]`

// jsonBatch is the most rows one statement carries.
const jsonBatch = 5000

// writeJSON runs q once per batch of rows, with args and then the batch as
// its parameters: the batch is a JSON array that q unnests with fromJSON,
// its last parameter.
func writeJSON[T any](w *writer, table, q string, rows []T, batch int, args ...any) {
	for len(rows) > 0 && w.err == nil {
		n := min(len(rows), batch)
		b, err := json.Marshal(rows[:n])
		if err != nil {
			w.fail(fmt.Errorf("%s: %w", table, err))
			return
		}
		w.exec(q, append(args[:len(args):len(args)], string(b))...)
		rows = rows[n:]
	}
}

// unixMicroPtr is unixMicro for an optional time.
func unixMicroPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return unixMicro(*t)
}

// flushJetStream writes one server's gathered JSZ rows. One INSERT per row
// and a lookup per configuration took about 7 ms per stream of two
// consumers on each server holding it (TestJetStreamWriteTime).
func (w *writer) flushJetStream(serverID string, b *jsRows) {
	// The server ID is a parameter: a NUL byte in it would end a string
	// literal in DuckDB's parser.
	head := fmt.Sprintf("%d, ?::VARCHAR", w.epoch)
	writeJSON(w, "stream_stats", `INSERT INTO stream_stats (epoch, server_id, account, stream, created, messages, bytes,
    first_seq, last_seq, first_ts, last_ts, num_subjects, num_deleted, consumer_count, cluster, raft_group, leader,
    leader_since, is_leader, replicas_reported, replicas_current, replicas_offline, mirror_lag, mirror_active_ns,
    mirror_error, sources_count, sources_max_lag)
SELECT `+head+`, account, stream, make_timestamp(created), messages, bytes, first_seq, last_seq,
    make_timestamp(first_ts), make_timestamp(last_ts), num_subjects, num_deleted, consumer_count, cluster, raft_group,
    leader, make_timestamp(leader_since), is_leader, replicas_reported, replicas_current, replicas_offline, mirror_lag,
    mirror_active_ns, mirror_error, sources_count, sources_max_lag
`+fromJSON(streamStatsShape), b.streams, jsonBatch, serverID)
	writeJSON(w, "stream_replicas", `INSERT INTO stream_replicas (epoch, server_id, account, stream, peer_name, peer_id,
    current, offline, active_ns, lag, pending)
SELECT `+head+`, account, stream, peer_name, peer_id, current, offline, active_ns, lag, pending
`+fromJSON(`[{"account":"VARCHAR","stream":"VARCHAR",`+replicaShape), b.streamReplicas, jsonBatch, serverID)
	writeJSON(w, "consumer_stats", `INSERT INTO consumer_stats (epoch, server_id, account, stream, consumer, created,
    delivered_consumer_seq, delivered_stream_seq, delivered_last_active, ack_floor_consumer_seq, ack_floor_stream_seq,
    ack_floor_last_active, num_ack_pending, num_redelivered, num_waiting, num_pending, push_bound, paused, cluster,
    raft_group, leader, leader_since, is_leader, replicas_reported, replicas_current, replicas_offline, reported_at)
SELECT `+head+`, account, stream, consumer, make_timestamp(created), delivered_consumer_seq, delivered_stream_seq,
    make_timestamp(delivered_last_active), ack_floor_consumer_seq, ack_floor_stream_seq,
    make_timestamp(ack_floor_last_active), num_ack_pending, num_redelivered, num_waiting, num_pending, push_bound,
    paused, cluster, raft_group, leader, make_timestamp(leader_since), is_leader, replicas_reported,
    replicas_current, replicas_offline, make_timestamp(reported_at)
`+fromJSON(consumerStatsShape), b.consumers, jsonBatch, serverID)
	writeJSON(w, "consumer_replicas", `INSERT INTO consumer_replicas (epoch, server_id, account, stream, consumer,
    peer_name, peer_id, current, offline, active_ns, lag, pending)
SELECT `+head+`, account, stream, consumer, peer_name, peer_id, current, offline, active_ns, lag, pending
`+fromJSON(`[{"account":"VARCHAR","stream":"VARCHAR","consumer":"VARCHAR",`+replicaShape), b.consumerReplicas, jsonBatch, serverID)

	writeConfigVersions(w, streamConfig, serverID, b.streamConfigs)
	writeConfigVersions(w, consumerConfig, serverID, b.consumerConfigs)
}

// configTable describes a configuration version table for
// writeConfigVersions.
type configTable struct {
	name  string
	keys  []string // the key after server_id
	cols  []string // the columns after config_hash
	shape string
}

var (
	streamConfig   = configTable{"stream_config", []string{"account", "stream"}, streamConfigCols, streamConfigShape}
	consumerConfig = configTable{"consumer_config", []string{"account", "stream", "consumer"}, consumerConfigCols, consumerConfigShape}
)

// writeConfigVersions extends the latest configuration row of each key
// when the hash and the creation time are unchanged and the row ends at
// the previous stored epoch, and starts a new row otherwise: when the
// configuration changed, is new, or the server reported nothing for the
// asset in between (server missing, JSZ missing, asset gone and back). A
// run therefore never covers an epoch in which the server did not report
// the configuration. A key with any run that already ends at this epoch
// is left as it is. The row-at-a-time write looked at the latest run only;
// the two differ only when an epoch is written after a newer one, which
// WriteSnapshot cannot do, as its epochs come from a sequence.
//
// The rows go to a staging table, as DuckDB cannot bind parameters in an
// UPDATE ... FROM: the staging INSERT binds the server ID, and the UPDATE
// and the INSERT from the staging table join on its server_id column, with
// only the epochs as literals. One server
// reports each of its assets once per epoch (repeated), so the staged keys
// are unique.
func writeConfigVersions[T any](w *writer, t configTable, serverID string, rows []T) {
	if len(rows) == 0 || w.err != nil {
		return
	}
	stage := "stage_" + t.name
	all := append(append(append([]string{"server_id"}, t.keys...), "first_epoch", "last_epoch", "config_hash"), t.cols...)
	sel := []string{"?::VARCHAR"}
	sel = append(sel, t.keys...)
	sel = append(sel, fmt.Sprint(w.epoch), fmt.Sprint(w.epoch), "config_hash")
	for _, c := range t.cols {
		if c == "created" {
			c = "make_timestamp(created)"
		}
		sel = append(sel, c)
	}
	on := func(a, b string) string {
		parts := []string{a + ".server_id = " + b + ".server_id"}
		for _, k := range t.keys {
			parts = append(parts, a+"."+k+" = "+b+"."+k)
		}
		return strings.Join(parts, " AND ")
	}
	cols := strings.Join(all, ", ")
	keys := strings.Join(t.keys, ", ")
	w.exec(fmt.Sprintf(`CREATE OR REPLACE TEMP TABLE %s AS SELECT %s FROM %s LIMIT 0`, stage, cols, t.name))
	writeJSON(w, t.name, fmt.Sprintf("INSERT INTO %s (%s)\nSELECT %s\n", stage, cols, strings.Join(sel, ", "))+
		fromJSON(t.shape), rows, jsonBatch, serverID)
	w.exec(fmt.Sprintf(`UPDATE %[1]s SET last_epoch = %[2]d
FROM (
    SELECT server_id, %[4]s, first_epoch FROM (
        SELECT c.*, s.config_hash AS new_hash, s.created AS new_created,
            row_number() OVER (PARTITION BY c.server_id, %[5]s ORDER BY c.first_epoch DESC) AS rn
        FROM %[1]s c JOIN %[6]s s ON %[7]s
    )
    WHERE rn = 1 AND last_epoch = %[3]d AND config_hash = new_hash AND created IS NOT DISTINCT FROM new_created
) x
WHERE %[8]s AND %[1]s.first_epoch = x.first_epoch`,
		t.name, w.epoch, w.prevEpoch, keys, "c."+strings.Join(t.keys, ", c."), stage, on("c", "s"), on(t.name, "x")))
	w.exec(fmt.Sprintf(`INSERT INTO %[1]s (%[2]s)
SELECT %[2]s FROM %[3]s s
WHERE NOT EXISTS (SELECT 1 FROM %[1]s c WHERE %[4]s AND c.last_epoch = %[5]d)`,
		t.name, cols, stage, on("c", "s"), w.epoch))
	w.exec(`DROP TABLE ` + stage)
}
