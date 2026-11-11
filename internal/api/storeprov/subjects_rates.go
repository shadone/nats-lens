package storeprov

import (
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// rateGap is the longest time between two subscription lists of a server
// that gives a delivered rate.
func (p *Provider) rateGap() time.Duration { return api.RateGap(p.SubscriptionsInterval) }

// rateCTEs are the common table expressions that end in rate: the
// delivered rate of each subscription of the collection at placeholder
// epoch, by api.DeliveredRates' rules, from its server's newest list
// before it that started at or after placeholder from. A server's lists
// both complete (listed not below total), the subscription in both with
// the same folded count and a count that did not go down; else the
// subscription has no row here. from is compared with the column
// directly, so the session time zone does not shift it.
func rateCTEs(epoch, from string) string {
	return fmt.Sprintf(`rlists AS (
  SELECT l.server_id, l.listed >= l.total AS complete, e.started_at AS t
  FROM subscription_lists l JOIN epochs e USING (epoch) WHERE l.epoch = %[1]s
), plists AS (
  SELECT l.server_id, l.epoch, l.listed >= l.total AS complete, e.started_at AS t
  FROM subscription_lists l JOIN epochs e USING (epoch)
  WHERE l.epoch < %[1]s AND e.started_at >= %[2]s
  QUALIFY row_number() OVER (PARTITION BY l.server_id ORDER BY l.epoch DESC) = 1
), pairs AS (
  SELECT c.server_id, p.epoch AS pepoch, date_diff('millisecond', p.t, c.t) AS ms
  FROM rlists c JOIN plists p USING (server_id) WHERE c.complete AND p.complete AND c.t > p.t
), rate AS (
  SELECT s.server_id, s.account, s.subject, s.queue, s.cid, (s.msgs - b.msgs) * 1000.0 / pr.ms AS r
  FROM subscriptions s JOIN pairs pr ON pr.server_id = s.server_id
  JOIN subscriptions b ON b.epoch = pr.pepoch AND b.server_id = s.server_id AND b.account = s.account AND b.subject = s.subject
    AND b.queue IS NOT DISTINCT FROM s.queue AND b.cid = s.cid
  WHERE s.epoch = %[1]s AND b.subs = s.subs AND s.msgs >= b.msgs AND pr.ms > 0
)`, epoch, from)
}

// rateList is the SQL of a group's connections with their rates, from a
// relation of one row per connection with name, server_name, cid and r
// (NULL without a rate), in api.SortConnectionRates' order, the first
// api.MaxConnectionRates.
func rateList() string {
	return fmt.Sprintf(`list({'name': coalesce(name, ''), 'server': server_name, 'cid': cid, 'r': r}
    ORDER BY r DESC NULLS LAST, coalesce(name, ''), server_name, cid)[1:%d]`, api.MaxConnectionRates)
}

// connectionRates reads a rateList value.
func connectionRates(v []any) ([]api.ConnectionRate, error) {
	out := make([]api.ConnectionRate, 0, len(v))
	for _, x := range v {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("connection rate %T", x)
		}
		var c api.ConnectionRate
		c.Name, _ = m["name"].(string)
		c.Server, _ = m["server"].(string)
		if cid, ok := m["cid"].(int64); ok {
			c.CID = uint64(cid)
		}
		if r, ok := m["r"].(float64); ok {
			c.PerSecond = &r
		}
		out = append(out, c)
	}
	return out, nil
}
