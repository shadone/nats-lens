package storeprov

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

// consumerRows reads the consumers of epoch e, optionally one account,
// stream or consumer.
func (p *Provider) consumerRows(ctx context.Context, e epochInfo, account, stream, consumer string) ([]*v1.ConsumerSummary, error) {
	var out []*v1.ConsumerSummary
	budget := newMetadataBudget()
	budget.rows = api.MaxListRankingRows
	err := p.readMetadata(ctx, `
SELECT x.account, x.stream, x.consumer,
  bool_or(coalesce(cc.durable_name, '') <> '') AS durable,
  coalesce(nullif(coalesce(max(cc.replicas) FILTER (WHERE x.is_leader), max(cc.replicas)), 0), any_value(sr.replicas)) AS replicas,
  max(x.num_pending) FILTER (WHERE x.is_leader) AS pending,
  coalesce(max(x.num_ack_pending) FILTER (WHERE x.is_leader), max(x.num_ack_pending)) AS ackpending,
  coalesce(max(x.num_redelivered) FILTER (WHERE x.is_leader), max(x.num_redelivered)) AS redelivered,
  max(x.num_waiting) FILTER (WHERE x.is_leader) AS waiting,
  coalesce(max(x.leader) FILTER (WHERE x.is_leader), max(x.leader) FILTER (WHERE x.leader <> '')) AS leader,
  coalesce(max(x.delivered_last_active) FILTER (WHERE x.is_leader), max(x.delivered_last_active)) AS last,
  coalesce(bool_or(x.paused), false) AS paused,
  count(DISTINCT cc.config_hash) > 1 OR count(DISTINCT x.created) > 1 AS disagree
FROM consumer_stats x LEFT JOIN consumer_config cc ON cc.server_id = x.server_id AND cc.account = x.account
  AND cc.stream = x.stream AND cc.consumer = x.consumer AND x.epoch BETWEEN cc.first_epoch AND cc.last_epoch
LEFT JOIN (
  SELECT c.account, c.stream, coalesce(max(c.replicas) FILTER (WHERE s.is_leader), max(c.replicas)) AS replicas
  FROM stream_config c LEFT JOIN stream_stats s ON s.epoch = ? AND s.server_id = c.server_id AND s.account = c.account
    AND s.stream = c.stream
  WHERE ? BETWEEN c.first_epoch AND c.last_epoch GROUP BY ALL) sr ON sr.account = x.account AND sr.stream = x.stream
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) AND (? = '' OR x.consumer = ?)
GROUP BY x.account, x.stream, x.consumer ORDER BY x.account, x.stream, x.consumer`, []any{e.epoch, e.epoch, e.epoch, account, account, stream, stream, consumer, consumer}, budget, func(raw []byte) error {
		var v struct {
			Account, Stream, Consumer, Leader         string
			Last                                      *string
			Durable, Paused, Disagree                 bool
			Replicas                                  *int32
			Pending, Ackpending, Redelivered, Waiting *int64
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		c := &v1.ConsumerSummary{Entity: refOf(v1.EntityType_ENTITY_TYPE_CONSUMER, []any{v.Account, v.Stream, v.Consumer}),
			Durable: v.Durable, Replicas: v.Replicas, NumPending: v.Pending, NumAckPending: v.Ackpending, NumRedelivered: v.Redelivered,
			NumWaiting: v.Waiting, Leader: v.Leader, Paused: v.Paused, ServersDisagree: v.Disagree, Present: true, Status: v1.Status_STATUS_NO_DATA}
		if v.Last != nil {
			last, err := time.Parse("2006-01-02 15:04:05.999999999", *v.Last)
			if err != nil {
				return err
			}
			c.LastDelivered = ts(last)
		}
		out = append(out, c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("consumers: %w", err)
	}
	return out, nil
}

// ListConsumers reads the consumers of the range's newest epoch.
func (p *Provider) ListConsumers(ctx context.Context, req *v1.ListConsumersRequest) (*v1.ListConsumersResponse, error) {
	n, err := api.EntityLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	scopeReq := proto.Clone(req).(*v1.ListConsumersRequest)
	scopeReq.Limit = 0
	scopeReq.PageToken = ""
	snap, offset, err := p.listPages.Begin(ctx, req.GetPageToken(), api.EntityScope(scopeReq), func() (*api.EntitySnapshot, error) {
		e, ok, err := p.latestEpoch(ctx, req.GetRange())
		if err != nil {
			return nil, err
		}
		snap := &api.EntitySnapshot{Epoch: e.epoch, Range: req.GetRange()}
		if !ok {
			return snap, nil
		}
		all, err := p.consumersWithMissing(ctx, req.GetRange(), e, req.GetAccount(), req.GetStream(), "")
		if err != nil {
			return nil, err
		}
		for _, c := range all {
			if c.GetDurable() || req.GetIncludeEphemeral() {
				snap.Consumers = append(snap.Consumers, c)
			}
		}
		snap.Consumers = api.SortConsumers(snap.Consumers, req.GetSort(), 0)
		return snap, nil
	})
	if err != nil {
		return nil, err
	}
	if err = p.listEpochStored(ctx, snap.Epoch); err != nil {
		return nil, err
	}
	res := &v1.ListConsumersResponse{TotalCount: int32(len(snap.Consumers)), Range: snap.Range, Epoch: snap.Epoch}
	for _, c := range snap.Consumers[offset:min(offset+n, len(snap.Consumers))] {
		res.Consumers = append(res.Consumers, proto.Clone(c).(*v1.ConsumerSummary))
	}
	for !api.FitsEntityPage(res) && len(res.Consumers) > 1 {
		res.Consumers = res.Consumers[:len(res.Consumers)-1]
	}
	if !api.FitsEntityPage(res) {
		return nil, fmt.Errorf("%w: consumer summary exceeds page byte budget", api.ErrResourceExhausted)
	}
	res.NextPageToken = snap.Next(offset + len(res.Consumers))
	return res, nil
}

// GetConsumer reads one consumer of the range's newest epoch.
func (p *Provider) GetConsumer(ctx context.Context, req *v1.GetConsumerRequest) (*v1.GetConsumerResponse, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	var list []*v1.ConsumerSummary
	if ok {
		if list, err = p.consumersWithMissing(ctx, req.GetRange(), e, req.GetAccount(), req.GetStream(), req.GetConsumer()); err != nil {
			return nil, err
		}
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("consumer %s/%s/%s in range: %w", req.GetAccount(), req.GetStream(), req.GetConsumer(), api.ErrNotFound)
	}
	res := &v1.GetConsumerResponse{Consumer: list[0]}
	cur, err := p.firingFindings(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	res.Findings = findingsFor(cur, res.GetConsumer().GetEntity())
	if !res.GetConsumer().GetPresent() {
		// A missing consumer has no server views; its configuration is the
		// inventory's.
		inv, _, err := p.inventory(ctx)
		if err != nil {
			return nil, err
		}
		res.Config = inventoryConfig(inv, res.GetConsumer().GetEntity().GetKey())
		return res, nil
	}
	if res.Servers, err = p.consumerViews(ctx, e.epoch, req.GetAccount(), req.GetStream(), req.GetConsumer()); err != nil {
		return nil, err
	}
	res.Disagreements = disagreements(res.GetServers(), func(v *v1.ConsumerServerView) (string, map[string]string) {
		return v.GetServer(), map[string]string{"config_hash": v.GetConfigHash(), "created": fmtTime(v.GetCreated().AsTime())}
	})
	res.Config, err = p.consumerConfig(ctx, e.epoch, req.GetAccount(), req.GetStream(), req.GetConsumer())
	return res, err
}

// consumerViews reads each server's view of a consumer; current, offline
// and lag come from the replica list of the leader placementOf chooses.
func (p *Provider) consumerViews(ctx context.Context, epoch int64, account, stream, consumer string) ([]*v1.ConsumerServerView, error) {
	m, err := p.consumerViewsBy(ctx, epoch, account, stream, consumer)
	return m[account+"/"+stream+"/"+consumer], err
}

// consumerViewsBy reads each server's view of every consumer of the
// epoch, optionally one account, stream or consumer, keyed by consumer
// key.
func (p *Provider) consumerViewsBy(ctx context.Context, epoch int64, account, stream, consumer string) (map[string][]*v1.ConsumerServerView, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH chosen AS (
  -- The leader placementOf chooses: the first server by name whose own
  -- report claims the lead, whether or not it lists replicas.
  SELECT l.account, l.stream, l.consumer, arg_min(l.server_id, ls.server_name) AS server_id
  FROM consumer_stats l JOIN servers ls USING (server_id)
  WHERE l.epoch = ? AND l.is_leader GROUP BY ALL),
lead AS (
  SELECT rp.account, rp.stream, rp.consumer, rp.peer_name, rp.current, rp.offline, rp.lag FROM consumer_replicas rp
  JOIN chosen c ON c.server_id = rp.server_id AND c.account = rp.account AND c.stream = rp.stream AND c.consumer = rp.consumer
  WHERE rp.epoch = ? AND (? = '' OR rp.account = ?) AND (? = '' OR rp.stream = ?) AND (? = '' OR rp.consumer = ?))
SELECT x.account, x.stream, x.consumer, s.server_name, coalesce(x.is_leader, false), x.delivered_stream_seq, x.ack_floor_stream_seq,
  x.created, cc.config_hash, r.current, r.offline, r.lag
FROM consumer_stats x JOIN servers s USING (server_id)
LEFT JOIN consumer_config cc ON cc.server_id = x.server_id AND cc.account = x.account AND cc.stream = x.stream
  AND cc.consumer = x.consumer AND x.epoch BETWEEN cc.first_epoch AND cc.last_epoch
LEFT JOIN lead r ON r.account = x.account AND r.stream = x.stream AND r.consumer = x.consumer AND r.peer_name = s.server_name
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) AND (? = '' OR x.consumer = ?) ORDER BY s.server_name`,
		epoch, epoch, account, account, stream, stream, consumer, consumer, epoch, account, account, stream, stream, consumer, consumer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]*v1.ConsumerServerView{}
	for rows.Next() {
		var v v1.ConsumerServerView
		var acc, st, name string
		var created sql.NullTime
		var hash sql.NullString
		var current, offline sql.NullBool
		if err := rows.Scan(&acc, &st, &name, &v.Server, &v.IsLeader, &v.DeliveredStreamSeq, &v.AckFloorStreamSeq, &created, &hash,
			&current, &offline, &v.Lag); err != nil {
			return nil, err
		}
		v.Created, v.ConfigHash = ts(ntime(created)), hash.String
		v.Current, v.Offline = v.GetIsLeader() || current.Bool, offline.Bool
		key := acc + "/" + st + "/" + name
		out[key] = append(out[key], &v)
	}
	return out, rows.Err()
}

// configValues turns one scanned row into settings, skipping nulls. Each
// column is named as NATS spells the setting; *_ns columns are durations.
func configValues(rows *sql.Rows) ([]*v1.ConfigValue, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	var out []*v1.ConfigValue
	for i, c := range cols {
		if vals[i] == nil {
			continue
		}
		key, value := c, fmt.Sprint(vals[i])
		if k, ok := strings.CutSuffix(c, "_ns"); ok {
			if n, ok := vals[i].(int64); ok {
				key, value = k, time.Duration(n).String()
			}
		}
		if list, ok := vals[i].([]any); ok {
			parts := make([]string, len(list))
			for j, x := range list {
				parts[j] = fmt.Sprint(x)
			}
			value = strings.Join(parts, ", ")
		}
		if n, ok := vals[i].(int32); ok {
			value = strconv.Itoa(int(n))
		}
		out = append(out, &v1.ConfigValue{Key: key, Value: value})
	}
	return out, nil
}

// leaderConfig reads one configuration row valid at the epoch, preferring
// the leader's.
func (p *Provider) leaderConfig(ctx context.Context, q string, args ...any) ([]*v1.ConfigValue, error) {
	rows, err := p.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return configValues(rows)
}

func (p *Provider) streamConfig(ctx context.Context, epoch int64, account, stream string) ([]*v1.ConfigValue, error) {
	return p.leaderConfig(ctx, `
SELECT c.subjects, c.retention, c.storage, c.replicas, c.max_consumers, c.max_msgs, c.max_bytes, c.max_age_ns,
  c.max_msgs_per_subject, c.max_msg_size, c.discard, c.duplicate_window_ns, c.mirror, c.sources, c.compression,
  c.sealed, c.deny_delete, c.deny_purge, c.allow_direct
FROM stream_config c LEFT JOIN stream_stats x ON x.epoch = ? AND x.server_id = c.server_id AND x.account = c.account AND x.stream = c.stream
WHERE c.account = ? AND c.stream = ? AND ? BETWEEN c.first_epoch AND c.last_epoch
ORDER BY coalesce(x.is_leader, false) DESC LIMIT 1`, epoch, account, stream, epoch)
}

func (p *Provider) consumerConfig(ctx context.Context, epoch int64, account, stream, consumer string) ([]*v1.ConfigValue, error) {
	return p.leaderConfig(ctx, `
SELECT c.durable_name, c.deliver_policy, c.ack_policy, c.ack_wait_ns, c.max_deliver, c.filter_subjects, c.replay_policy,
  c.max_waiting, c.max_ack_pending, c.max_batch, c.deliver_subject, c.deliver_group, c.inactive_threshold_ns,
  c.replicas, c.mem_storage
FROM consumer_config c LEFT JOIN consumer_stats x ON x.epoch = ? AND x.server_id = c.server_id AND x.account = c.account
  AND x.stream = c.stream AND x.consumer = c.consumer
WHERE c.account = ? AND c.stream = ? AND c.consumer = ? AND ? BETWEEN c.first_epoch AND c.last_epoch
ORDER BY coalesce(x.is_leader, false) DESC LIMIT 1`, epoch, account, stream, consumer, epoch)
}

// consumersWithMissing reads the consumers of epoch e and adds the
// inventory's consumers that no server reports, with statuses and finding
// codes from the firing findings, worst first.
func (p *Provider) consumersWithMissing(ctx context.Context, r *v1.TimeRange, e epochInfo, account, stream, consumer string) ([]*v1.ConsumerSummary, error) {
	cs, err := p.consumerRows(ctx, e, account, stream, consumer)
	if err != nil {
		return nil, err
	}
	rates, err := p.deliveredRatesLimited(ctx, r, account, stream, consumer, api.MaxListRankingRows)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if v, ok := rates[c.GetEntity().GetKey()]; ok {
			c.DeliveredPerSecond = ptr(v)
		}
	}
	inv, err := p.listConsumerInventory(ctx, account, stream, consumer)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, c := range cs {
		have[c.GetEntity().GetKey()] = true
	}
	for _, c := range inv {
		if have[c.key()] {
			continue
		}
		if len(cs) >= api.MaxListRankingRows {
			return nil, metadataOverflow()
		}
		cs = append(cs, missingConsumers([]expectedConsumer{c}, nil, account, stream, consumer)...)
		have[c.key()] = true
	}
	cur, err := p.listStatusFindings(ctx, r, consumerSummaryKeys(cs), false)
	if err != nil {
		return nil, err
	}
	ran, err := p.checksRan(ctx, e.epoch)
	if err != nil {
		return nil, err
	}
	applyConsumerStatus(cs, cur, ran)
	return cs, nil
}
