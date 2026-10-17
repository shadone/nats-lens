package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Bucket stream prefixes: a KV bucket is the stream KV_<bucket>, an object
// store OBJ_<bucket>.
const (
	kvPrefix  = "KV_"
	objPrefix = "OBJ_"
)

// bucketRow is a bucket's stream in the newest epoch, or a bucket the
// inventory lists that no server reports (present false, nothing else set).
type bucketRow struct {
	ref                  *v1.EntityRef
	bucket               string
	present, expected    bool
	messages, bytes      sql.NullInt64
	subjects             sql.NullInt64
	consumers            sql.NullInt32 // from the consumer detail; null without any
	reported             sql.NullInt64 // the servers' own consumer count
	description, storage sql.NullString
	compression, mirror  sql.NullString
	replicas             sql.NullInt32
	maxBytes, maxAgeNs   sql.NullInt64
	history              sql.NullInt64
	sealed, allowMsgTTL  sql.NullBool
	hasConfig            bool
	placement            []*v1.ReplicaPlacement
	bytesPerDay          *float64
	status               v1.Status
	codes                []string
}

// ListKeyValueStores reads the KV buckets of the range's newest epoch and
// the ones the inventory lists that no server reports.
func (p *Provider) ListKeyValueStores(ctx context.Context, req *v1.ListKeyValueStoresRequest) (*v1.ListKeyValueStoresResponse, error) {
	rows, err := p.buckets(ctx, req.GetRange(), req.GetAccount(), kvPrefix, "kv")
	if err != nil {
		return nil, err
	}
	res := &v1.ListKeyValueStoresResponse{}
	for _, b := range rows {
		kv := &v1.KeyValueStore{
			Entity: b.ref, Bucket: b.bucket, Present: b.present, Expected: b.expected,
			Keys: nullI64(b.subjects), Bytes: nullI64(b.bytes), Messages: nullI64(b.messages),
			Status: b.status, FindingCodes: b.codes, Placement: b.placement, BytesPerDay: b.bytesPerDay,
			Storage: b.storage.String, Compression: b.compression.String, Mirror: b.mirror.String,
			AllowMsgTtl: b.allowMsgTTL.Bool, Description: b.description.String,
		}
		kv.Watchers = watchers(b)
		if b.hasConfig {
			kv.Replicas, kv.MaxBytes, kv.Ttl = nullI32(b.replicas), nullI64(b.maxBytes), ttlOf(b.maxAgeNs)
			if b.history.Valid {
				kv.History = ptr(int32(b.history.Int64))
			}
		}
		res.KeyValueStores = append(res.KeyValueStores, kv)
	}
	return res, nil
}

// ListObjectStores reads the object stores of the range's newest epoch and
// the ones the inventory lists that no server reports.
func (p *Provider) ListObjectStores(ctx context.Context, req *v1.ListObjectStoresRequest) (*v1.ListObjectStoresResponse, error) {
	rows, err := p.buckets(ctx, req.GetRange(), req.GetAccount(), objPrefix, "object_store")
	if err != nil {
		return nil, err
	}
	res := &v1.ListObjectStoresResponse{}
	for _, b := range rows {
		o := &v1.ObjectStore{
			Entity: b.ref, Bucket: b.bucket, Present: b.present, Expected: b.expected,
			Bytes: nullI64(b.bytes), Chunks: nullI64(b.messages), Status: b.status, FindingCodes: b.codes,
			Placement: b.placement, BytesPerDay: b.bytesPerDay, Storage: b.storage.String,
			Sealed: b.sealed.Bool, Description: b.description.String,
		}
		if b.hasConfig {
			o.Replicas, o.MaxBytes, o.Ttl = nullI32(b.replicas), nullI64(b.maxBytes), ttlOf(b.maxAgeNs)
		}
		res.ObjectStores = append(res.ObjectStores, o)
	}
	return res, nil
}

// buckets reads the buckets whose streams start with prefix, adds the
// missing ones of the inventory's kind, and sets status and placement;
// worst first.
func (p *Provider) buckets(ctx context.Context, r *v1.TimeRange, account, prefix, kind string) ([]*bucketRow, error) {
	e, ok, err := p.latestEpoch(ctx, r)
	if err != nil {
		return nil, err
	}
	var out []*bucketRow
	if ok {
		if out, err = p.presentBuckets(ctx, e.epoch, account, prefix); err != nil {
			return nil, err
		}
		if err := p.bucketPlacement(ctx, e.epoch, account, prefix, out); err != nil {
			return nil, err
		}
		start, end := span(r)
		growth, err := p.streamGrowth(ctx, start, end, account, "")
		if err != nil {
			return nil, err
		}
		for _, b := range out {
			b.bytesPerDay = robustPerDay(growth[b.ref.GetKey()])
		}
	}
	expected, err := p.expectedBuckets(ctx, kind)
	if err != nil {
		return nil, err
	}
	have := map[string]*bucketRow{}
	for _, b := range out {
		have[b.ref.GetKey()] = b
	}
	for _, x := range expected {
		if account != "" && x.account != account {
			continue
		}
		key := x.account + "/" + x.stream
		if b := have[key]; b != nil {
			b.expected = true
			continue
		}
		b := &bucketRow{ref: refOf(v1.EntityType_ENTITY_TYPE_STREAM, []any{x.account, x.stream}), bucket: x.name, expected: true}
		have[key] = b
		out = append(out, b)
	}
	cur, err := p.firingFindings(ctx, r)
	if err != nil {
		return nil, err
	}
	var ran ranFor = func(...string) bool { return false }
	if ok {
		if ran, err = p.checksRan(ctx, e.epoch); err != nil {
			return nil, err
		}
	}
	for _, b := range out {
		st, codes := statusOf(cur, b.ref)
		st = orNoData(st, ran("stream"))
		if !b.present && len(codes) == 0 {
			st = v1.Status_STATUS_NO_DATA
		}
		b.status, b.codes = st, codes
	}
	slices.SortStableFunc(out, func(a, b *bucketRow) int {
		return cmp.Or(cmp.Compare(statusRank(a.status), statusRank(b.status)), cmp.Compare(a.ref.GetKey(), b.ref.GetKey()))
	})
	return out, nil
}

// presentBuckets reads the bucket streams of an epoch: the leader's numbers
// (the largest without a leader), the leader's configuration (the first
// server's by name without one), and the consumers, which for a bucket are
// its watchers.
func (p *Provider) presentBuckets(ctx context.Context, epoch int64, account, prefix string) ([]*bucketRow, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH now_ AS (
  SELECT account, stream,
    coalesce(max(messages) FILTER (WHERE is_leader), max(messages)) AS messages,
    coalesce(max(bytes) FILTER (WHERE is_leader), max(bytes)) AS bytes,
    coalesce(max(num_subjects) FILTER (WHERE is_leader), max(num_subjects)) AS subjects,
    coalesce(max(consumer_count) FILTER (WHERE is_leader), max(consumer_count)) AS reported
  FROM stream_stats WHERE epoch = ? AND (? = '' OR account = ?) AND starts_with(stream, ?) AND length(stream) > length(?)
  GROUP BY ALL),
cfg AS (
  SELECT c.account, c.stream, c.description, c.storage, c.compression, c.mirror, c.replicas, c.max_bytes,
    c.max_age_ns, c.max_msgs_per_subject, c.sealed, c.allow_msg_ttl
  FROM stream_config c JOIN servers s USING (server_id)
  LEFT JOIN stream_stats x ON x.epoch = ? AND x.server_id = c.server_id AND x.account = c.account AND x.stream = c.stream
  WHERE ? BETWEEN c.first_epoch AND c.last_epoch AND starts_with(c.stream, ?)
  QUALIFY row_number() OVER (PARTITION BY c.account, c.stream ORDER BY coalesce(x.is_leader, false) DESC, s.server_name) = 1),
cons AS (
  SELECT account, stream, count(DISTINCT consumer)::INTEGER AS n
  FROM consumer_stats WHERE epoch = ? AND starts_with(stream, ?) GROUP BY ALL)
SELECT n.account, n.stream, n.messages, n.bytes, n.subjects, k.n, n.reported, c.stream IS NOT NULL,
  c.description, c.storage, c.compression, c.mirror, c.replicas, c.max_bytes, c.max_age_ns, c.max_msgs_per_subject,
  c.sealed, c.allow_msg_ttl
FROM now_ n LEFT JOIN cfg c USING (account, stream) LEFT JOIN cons k USING (account, stream)
ORDER BY n.account, n.stream`,
		epoch, account, account, prefix, prefix, epoch, epoch, prefix, epoch, prefix)
	if err != nil {
		return nil, fmt.Errorf("buckets: %w", err)
	}
	defer rows.Close()
	var out []*bucketRow
	for rows.Next() {
		b := &bucketRow{present: true}
		var acc, stream string
		if err := rows.Scan(&acc, &stream, &b.messages, &b.bytes, &b.subjects, &b.consumers, &b.reported, &b.hasConfig,
			&b.description, &b.storage, &b.compression, &b.mirror, &b.replicas, &b.maxBytes, &b.maxAgeNs, &b.history,
			&b.sealed, &b.allowMsgTTL); err != nil {
			return nil, err
		}
		b.ref, b.bucket = refOf(v1.EntityType_ENTITY_TYPE_STREAM, []any{acc, stream}), strings.TrimPrefix(stream, prefix)
		out = append(out, b)
	}
	return out, rows.Err()
}

// replicaView is what is known of one replica of a bucket's stream: the
// server's own report, and the leader's entry for it.
type replicaView struct {
	reports, leader  bool
	hash             string
	listed           bool
	current, offline bool
}

// bucketPlacement sets each bucket's placement from the servers' reports
// and the leader's replica list.
func (p *Provider) bucketPlacement(ctx context.Context, epoch int64, account, prefix string, bs []*bucketRow) error {
	rows, err := p.db().QueryContext(ctx, `
SELECT x.account, x.stream, s.server_name, true AS reports, coalesce(x.is_leader, false), coalesce(sc.config_hash, ''),
  false AS listed, false, false
FROM stream_stats x JOIN servers s USING (server_id)
LEFT JOIN stream_config sc ON sc.server_id = x.server_id AND sc.account = x.account AND sc.stream = x.stream
  AND x.epoch BETWEEN sc.first_epoch AND sc.last_epoch
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND starts_with(x.stream, ?)
UNION ALL
SELECT rp.account, rp.stream, rp.peer_name, false, false, '', true, coalesce(rp.current, false), coalesce(rp.offline, false)
FROM stream_replicas rp
JOIN stream_stats l ON l.epoch = rp.epoch AND l.server_id = rp.server_id AND l.account = rp.account
  AND l.stream = rp.stream AND l.is_leader
WHERE rp.epoch = ? AND (? = '' OR rp.account = ?) AND starts_with(rp.stream, ?)`,
		epoch, account, account, prefix, epoch, account, account, prefix)
	if err != nil {
		return fmt.Errorf("bucket placement: %w", err)
	}
	defer rows.Close()
	views := map[string]map[string]*replicaView{}
	for rows.Next() {
		var acc, stream, server string
		var r replicaView
		if err := rows.Scan(&acc, &stream, &server, &r.reports, &r.leader, &r.hash, &r.listed, &r.current, &r.offline); err != nil {
			return err
		}
		key := acc + "/" + stream
		if views[key] == nil {
			views[key] = map[string]*replicaView{}
		}
		v := views[key][server]
		if v == nil {
			v = &replicaView{}
			views[key][server] = v
		}
		if r.reports {
			v.reports, v.leader, v.hash = true, v.leader || r.leader, r.hash
		}
		if r.listed {
			v.listed, v.current, v.offline = true, r.current, r.offline
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, b := range bs {
		b.placement = placement(views[b.ref.GetKey()])
	}
	return nil
}

// placement lists one bucket's replicas: the leader, then its followers by
// name. A follower is offline as the leader lists it, missing when it
// reports nothing, disagrees when the leader does not list it, behind when
// the leader lists it as not current, and otherwise disagrees when its
// configuration differs from the leader's and is in sync when it does not.
// Without a leader a server's state is unknown unless its configuration
// differs from another's.
func placement(vs map[string]*replicaView) []*v1.ReplicaPlacement {
	// Two servers may both claim the lead in one epoch; the first by name
	// leads, so the result does not follow map order.
	leader, leaderHash := "", ""
	for name, v := range vs {
		if v.leader && (leader == "" || name < leader) {
			leader, leaderHash = name, v.hash
		}
	}
	names := make([]string, 0, len(vs))
	for name := range vs {
		if name != leader {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var out []*v1.ReplicaPlacement
	if leader != "" {
		out = append(out, &v1.ReplicaPlacement{Server: leader, State: v1.PlacementState_PLACEMENT_STATE_LEADER})
	}
	for _, name := range names {
		v := vs[name]
		var st v1.PlacementState
		switch {
		case leader == "":
			if v.hash != "" && slices.ContainsFunc(names, func(o string) bool { return vs[o].hash != "" && vs[o].hash != v.hash }) {
				st = v1.PlacementState_PLACEMENT_STATE_DISAGREES
			}
		case v.listed && v.offline:
			st = v1.PlacementState_PLACEMENT_STATE_OFFLINE
		case !v.reports:
			st = v1.PlacementState_PLACEMENT_STATE_MISSING
		case !v.listed:
			st = v1.PlacementState_PLACEMENT_STATE_DISAGREES
		case !v.current:
			// A replica catching up may still hold the old configuration.
			st = v1.PlacementState_PLACEMENT_STATE_BEHIND
		case v.hash != "" && leaderHash != "" && v.hash != leaderHash:
			st = v1.PlacementState_PLACEMENT_STATE_DISAGREES
		default:
			st = v1.PlacementState_PLACEMENT_STATE_IN_SYNC
		}
		out = append(out, &v1.ReplicaPlacement{Server: name, State: st})
	}
	return out
}

// expectedBucket is a bucket the inventory lists, with its account resolved
// as the inventory checks do.
type expectedBucket struct{ account, name, stream string }

// expectedBuckets reads the inventory's buckets of one kind.
func (p *Provider) expectedBuckets(ctx context.Context, kind string) ([]expectedBucket, error) {
	rows, err := p.db().QueryContext(ctx, `
SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                 ORDER BY (a.account = i.account) DESC LIMIT 1), i.account) AS acct, i.name, i.stream
FROM inventory i WHERE i.kind = ? ORDER BY acct, i.stream`, kind)
	if err != nil {
		return nil, fmt.Errorf("inventory buckets: %w", err)
	}
	defer rows.Close()
	var out []expectedBucket
	for rows.Next() {
		var b expectedBucket
		if err := rows.Scan(&b.account, &b.name, &b.stream); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// watchers counts the consumers in the JSZ detail of a present bucket's
// stream. Without any in the detail a reported zero is zero and any other
// count is unset, as the detail of those consumers was not collected.
func watchers(b *bucketRow) *int32 {
	switch {
	case !b.present:
		return nil
	case b.consumers.Valid:
		return ptr(b.consumers.Int32)
	case b.reported.Valid && b.reported.Int64 == 0:
		return ptr(int32(0))
	}
	return nil
}

func nullI64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func nullI32(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

// ttlOf is max_age as a duration; unset when not reported.
func ttlOf(ns sql.NullInt64) *durationpb.Duration {
	if !ns.Valid {
		return nil
	}
	return durationpb.New(time.Duration(ns.Int64))
}
