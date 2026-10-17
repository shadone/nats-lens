package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// expectedConsumer is a consumer the inventory lists, with its account
// resolved to the one the servers report, as the inventory checks do.
type expectedConsumer struct {
	account, stream, consumer string
	replicas                  sql.NullInt32
	ackWaitNs                 sql.NullInt64
	maxDeliver, maxAckPending sql.NullInt32
	filters                   []any
}

func (c expectedConsumer) key() string { return c.account + "/" + c.stream + "/" + c.consumer }

// inventory reads the expected consumers, and which streams the inventory
// names at all (as a stream, bucket or the stream of a consumer).
func (p *Provider) inventory(ctx context.Context) ([]expectedConsumer, map[string]bool, error) {
	rows, err := p.db().QueryContext(ctx, `
SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                 ORDER BY (a.account = i.account) DESC LIMIT 1), i.account) AS acct,
  i.kind, i.stream, coalesce(i.consumer, ''), i.replicas, i.ack_wait_ns, i.max_deliver, i.max_ack_pending,
  coalesce(i.filter_subjects, [])
FROM inventory i ORDER BY acct, i.stream, i.consumer`)
	if err != nil {
		return nil, nil, fmt.Errorf("inventory: %w", err)
	}
	defer rows.Close()
	var out []expectedConsumer
	streams := map[string]bool{}
	for rows.Next() {
		var c expectedConsumer
		var kind string
		if err := rows.Scan(&c.account, &kind, &c.stream, &c.consumer, &c.replicas, &c.ackWaitNs, &c.maxDeliver,
			&c.maxAckPending, &c.filters); err != nil {
			return nil, nil, err
		}
		streams[c.account+"/"+c.stream] = true
		if kind == "consumer" {
			out = append(out, c)
		}
	}
	return out, streams, rows.Err()
}

// expectedPerStream counts the inventory's consumers per account/stream.
func expectedPerStream(inv []expectedConsumer) map[string]int32 {
	out := map[string]int32{}
	for _, c := range inv {
		out[c.account+"/"+c.stream]++
	}
	return out
}

// missingConsumers returns the expected consumers that are not among
// present, optionally of one account and stream, as summaries with
// present unset and no numbers.
func missingConsumers(inv []expectedConsumer, present []*v1.ConsumerSummary, account, stream, consumer string) []*v1.ConsumerSummary {
	have := map[string]bool{}
	for _, c := range present {
		have[c.GetEntity().GetKey()] = true
	}
	var out []*v1.ConsumerSummary
	for _, c := range inv {
		if have[c.key()] || (account != "" && c.account != account) || (stream != "" && c.stream != stream) ||
			(consumer != "" && c.consumer != consumer) {
			continue
		}
		s := &v1.ConsumerSummary{
			Entity:  refOf(v1.EntityType_ENTITY_TYPE_CONSUMER, []any{c.account, c.stream, c.consumer}),
			Durable: true, Present: false, Status: v1.Status_STATUS_NO_DATA,
		}
		if c.replicas.Valid {
			s.Replicas = &c.replicas.Int32
		}
		out = append(out, s)
	}
	return out
}

// inventoryConfig is the expected configuration of a missing consumer.
func inventoryConfig(inv []expectedConsumer, key string) []*v1.ConfigValue {
	for _, c := range inv {
		if c.key() != key {
			continue
		}
		out := []*v1.ConfigValue{{Key: "durable_name", Value: c.consumer}}
		if c.ackWaitNs.Valid {
			out = append(out, &v1.ConfigValue{Key: "ack_wait", Value: time.Duration(c.ackWaitNs.Int64).String()})
		}
		if c.maxDeliver.Valid {
			out = append(out, &v1.ConfigValue{Key: "max_deliver", Value: strconv.Itoa(int(c.maxDeliver.Int32))})
		}
		if c.maxAckPending.Valid {
			out = append(out, &v1.ConfigValue{Key: "max_ack_pending", Value: strconv.Itoa(int(c.maxAckPending.Int32))})
		}
		if len(c.filters) > 0 {
			parts := make([]string, len(c.filters))
			for i, f := range c.filters {
				parts[i] = fmt.Sprint(f)
			}
			out = append(out, &v1.ConfigValue{Key: "filter_subjects", Value: strings.Join(parts, ", ")})
		}
		if c.replicas.Valid {
			out = append(out, &v1.ConfigValue{Key: "replicas", Value: strconv.Itoa(int(c.replicas.Int32))})
		}
		return out
	}
	return nil
}
