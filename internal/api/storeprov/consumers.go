package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// consumerRows reads the consumers of epoch e, optionally one account,
// stream or consumer.
func (p *Provider) consumerRows(ctx context.Context, e epochInfo, account, stream, consumer string) ([]*v1.ConsumerSummary, error) {
	rows, err := p.db().QueryContext(ctx, `
SELECT x.account, x.stream, x.consumer,
  bool_or(coalesce(cc.durable_name, '') <> ''),
  coalesce(nullif(coalesce(max(cc.replicas) FILTER (WHERE x.is_leader), max(cc.replicas)), 0), any_value(sr.replicas)),
  max(x.num_pending) FILTER (WHERE x.is_leader),
  coalesce(max(x.num_ack_pending) FILTER (WHERE x.is_leader), max(x.num_ack_pending)),
  coalesce(max(x.num_redelivered) FILTER (WHERE x.is_leader), max(x.num_redelivered)),
  max(x.num_waiting) FILTER (WHERE x.is_leader),
  coalesce(max(x.leader) FILTER (WHERE x.is_leader), max(x.leader) FILTER (WHERE x.leader <> '')),
  coalesce(max(x.delivered_last_active) FILTER (WHERE x.is_leader), max(x.delivered_last_active)),
  coalesce(bool_or(x.paused), false),
  count(DISTINCT cc.config_hash) > 1 OR count(DISTINCT x.created) > 1
FROM consumer_stats x LEFT JOIN consumer_config cc ON cc.server_id = x.server_id AND cc.account = x.account
  AND cc.stream = x.stream AND cc.consumer = x.consumer AND x.epoch BETWEEN cc.first_epoch AND cc.last_epoch
LEFT JOIN (
  SELECT c.account, c.stream, coalesce(max(c.replicas) FILTER (WHERE s.is_leader), max(c.replicas)) AS replicas
  FROM stream_config c LEFT JOIN stream_stats s ON s.epoch = ? AND s.server_id = c.server_id AND s.account = c.account
    AND s.stream = c.stream
  WHERE ? BETWEEN c.first_epoch AND c.last_epoch GROUP BY ALL) sr ON sr.account = x.account AND sr.stream = x.stream
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) AND (? = '' OR x.consumer = ?)
GROUP BY x.account, x.stream, x.consumer ORDER BY x.account, x.stream, x.consumer`,
		e.epoch, e.epoch, e.epoch, account, account, stream, stream, consumer, consumer)
	if err != nil {
		return nil, fmt.Errorf("consumers: %w", err)
	}
	defer rows.Close()
	var out []*v1.ConsumerSummary
	for rows.Next() {
		var c v1.ConsumerSummary
		var acc, st, name string
		var leader sql.NullString
		var last sql.NullTime
		// Numbers scan into their optional fields: NULL leaves them unset.
		// Pending and waiting come from the leader's row only, because
		// followers report zero for both. Replicas is the effective count:
		// a consumer configured with 0 replicas has its stream's. The
		// leader and the last delivery are the leader's row's, a
		// follower's otherwise.
		if err := rows.Scan(&acc, &st, &name, &c.Durable, &c.Replicas, &c.NumPending, &c.NumAckPending, &c.NumRedelivered,
			&c.NumWaiting, &leader, &last, &c.Paused, &c.ServersDisagree); err != nil {
			return nil, err
		}
		c.Entity = refOf(v1.EntityType_ENTITY_TYPE_CONSUMER, []any{acc, st, name})
		// The status is no data until applyConsumerStatus applies the findings.
		c.Leader, c.LastDelivered, c.Present, c.Status = leader.String, ts(ntime(last)), true, v1.Status_STATUS_NO_DATA
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ListConsumers reads the consumers of the range's newest epoch.
func (p *Provider) ListConsumers(ctx context.Context, req *v1.ListConsumersRequest) (*v1.ListConsumersResponse, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &v1.ListConsumersResponse{}, err
	}
	all, err := p.consumersWithMissing(ctx, req.GetRange(), e, req.GetAccount(), req.GetStream(), "")
	if err != nil {
		return nil, err
	}
	res := &v1.ListConsumersResponse{}
	for _, c := range all {
		if c.GetDurable() || req.GetIncludeEphemeral() {
			res.Consumers = append(res.Consumers, c)
		}
	}
	res.Consumers = api.SortConsumers(res.Consumers, req.GetSort(), req.GetLimit())
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
WITH lead AS (
  SELECT rp.account, rp.stream, rp.consumer, rp.peer_name, rp.current, rp.offline, rp.lag FROM consumer_replicas rp
  JOIN consumer_stats l ON l.epoch = rp.epoch AND l.server_id = rp.server_id AND l.account = rp.account
    AND l.stream = rp.stream AND l.consumer = rp.consumer AND l.is_leader
  JOIN servers ls ON ls.server_id = rp.server_id
  WHERE rp.epoch = ? AND (? = '' OR rp.account = ?) AND (? = '' OR rp.stream = ?) AND (? = '' OR rp.consumer = ?)
  -- Only the leader placementOf chooses, the first claimant by name.
  QUALIFY ls.server_name = min(ls.server_name) OVER (PARTITION BY rp.account, rp.stream, rp.consumer))
SELECT x.account, x.stream, x.consumer, s.server_name, coalesce(x.is_leader, false), x.delivered_stream_seq, x.ack_floor_stream_seq,
  x.created, cc.config_hash, r.current, r.offline, r.lag
FROM consumer_stats x JOIN servers s USING (server_id)
LEFT JOIN consumer_config cc ON cc.server_id = x.server_id AND cc.account = x.account AND cc.stream = x.stream
  AND cc.consumer = x.consumer AND x.epoch BETWEEN cc.first_epoch AND cc.last_epoch
LEFT JOIN lead r ON r.account = x.account AND r.stream = x.stream AND r.consumer = x.consumer AND r.peer_name = s.server_name
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) AND (? = '' OR x.consumer = ?) ORDER BY s.server_name`,
		epoch, account, account, stream, stream, consumer, consumer, epoch, account, account, stream, stream, consumer, consumer)
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
	rates, err := p.deliveredRates(ctx, r, account, stream, consumer)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if v, ok := rates[c.GetEntity().GetKey()]; ok {
			c.DeliveredPerSecond = ptr(v)
		}
	}
	inv, _, err := p.inventory(ctx)
	if err != nil {
		return nil, err
	}
	cs = append(cs, missingConsumers(inv, cs, account, stream, consumer)...)
	cur, err := p.firingFindings(ctx, r)
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
