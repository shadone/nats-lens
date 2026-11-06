package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/routehealth"
)

// Topology reads the range's newest epoch: its servers, their ROUTEZ,
// GATEWAYZ and LEAFZ outcomes and rows, the routes of the collection
// before it for the slow rule, and the drops of the 24 hours before,
// counted in SQL (topology_drops.go).
// Leaf rows in which the reporting server is the spoke (its own uplink to
// a hub) are not sites of this system and are left out.
func (p *Provider) Topology(ctx context.Context, req *v1.GetTopologyRequest) (*api.TopologyFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.TopologyFacts{}, err
	}
	f := &api.TopologyFacts{Epoch: e.epoch, EpochAt: e.startedAt, Interval: p.Interval,
		Answered: map[string][]string{}, Unanswered: map[string][]string{}}
	steps := []func(context.Context, *api.TopologyFacts) error{
		p.topologyServers, p.topologyEndpoints, p.topologyRoutes, p.topologyEarlier, p.topologyGateways, p.topologyLeafs,
		p.topologyDrops,
	}
	for _, step := range steps {
		if err := step(ctx, f); err != nil {
			return nil, err
		}
	}
	f.MetaLeader, err = p.metaLeaderAt(ctx, e.epoch)
	return f, err
}

func durPtr(v sql.NullInt64) *time.Duration {
	if !v.Valid {
		return nil
	}
	d := time.Duration(v.Int64)
	return &d
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// topologyServers reads the servers the epoch expected, answered or
// missed, each with the cluster and tags of its process of the epoch (its
// last one up to it) and the streams it led then.
func (p *Provider) topologyServers(ctx context.Context, f *api.TopologyFacts) error {
	rows, err := p.db().QueryContext(ctx, `
WITH e AS (SELECT * FROM epochs WHERE epoch = ?),
names AS (SELECT DISTINCT unnest(expected_names || observed_names || missing_names) AS name FROM e),
procs AS (
  SELECT s.*, row_number() OVER (PARTITION BY server_name ORDER BY last_epoch DESC) AS rn
  FROM servers s WHERE first_epoch <= ? AND server_name IN (SELECT name FROM names))
SELECT n.name, coalesce(p.cluster, ''), coalesce(p.tags, []), list_contains((SELECT missing_names FROM e), n.name),
  ss.js_streams_leader
FROM names n
LEFT JOIN procs p ON p.server_name = n.name AND p.rn = 1
LEFT JOIN server_stats ss ON ss.server_id = p.server_id AND ss.epoch = ?
ORDER BY n.name`, f.Epoch, f.Epoch, f.Epoch)
	if err != nil {
		return fmt.Errorf("topology servers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s api.TopologyServerFact
		var tags []any
		var led sql.NullInt32
		if err := rows.Scan(&s.Name, &s.Cluster, &tags, &s.Missing, &led); err != nil {
			return err
		}
		s.Tags = strs(tags)
		if led.Valid {
			s.StreamsLed = &led.Int32
		}
		f.Servers = append(f.Servers, s)
	}
	return rows.Err()
}

// topologyEndpoints reads which servers' ROUTEZ, GATEWAYZ and LEAFZ
// answered in the epoch and which did not.
func (p *Provider) topologyEndpoints(ctx context.Context, f *api.TopologyFacts) error {
	rows, err := p.db().QueryContext(ctx, `
SELECT DISTINCT s.server_name, ee.endpoint, ee.state = 'ok'
FROM epoch_endpoints ee JOIN servers s USING (server_id)
WHERE ee.epoch = ? AND ee.endpoint IN (?, ?, ?)
ORDER BY 1`, f.Epoch, api.EndpointRoutez, api.EndpointGatewayz, api.EndpointLeafz)
	if err != nil {
		return fmt.Errorf("topology endpoints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, endpoint string
		var ok bool
		if err := rows.Scan(&name, &endpoint, &ok); err != nil {
			return err
		}
		if ok {
			f.Answered[endpoint] = append(f.Answered[endpoint], name)
		} else {
			f.Unanswered[endpoint] = append(f.Unanswered[endpoint], name)
		}
	}
	return rows.Err()
}

// topologyRoutes reads the epoch's routes.
func (p *Provider) topologyRoutes(ctx context.Context, f *api.TopologyFacts) error {
	var err error
	f.Routes, err = p.routeRows(ctx, f.Epoch)
	return err
}

// topologyEarlier reads the collection the slow rule compares the epoch
// with: the newest epoch before it with route rows that started no
// earlier than api.TopologyEarliestEarlier, and its routes; none when the
// collect interval is not known. The start is compared with the column
// directly, so the session time zone does not shift it.
func (p *Provider) topologyEarlier(ctx context.Context, f *api.TopologyFacts) error {
	var err error
	f.Earlier, err = routehealth.ReadEarlier(ctx, p.db(), f.Epoch, f.EpochAt, p.Interval)
	return err
}

func (p *Provider) routeRows(ctx context.Context, epoch int64) ([]api.TopologyRouteRow, error) {
	return routehealth.ReadRows(ctx, p.db(), epoch)
}

// topologyGateways reads the epoch's gateway rows, both directions.
func (p *Provider) topologyGateways(ctx context.Context, f *api.TopologyFacts) error {
	rows, err := p.db().QueryContext(ctx, `
SELECT g.server_id, s.server_name, coalesce(s.cluster, ''), g.gateway, g.direction = 'outbound', g.cid, g.rtt_ns, g.pending_bytes
FROM gateways g JOIN servers s USING (server_id) WHERE g.epoch = ?`, f.Epoch)
	if err != nil {
		return fmt.Errorf("topology gateways: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r api.TopologyGatewayRow
		var cid, rtt, pending sql.NullInt64
		if err := rows.Scan(&r.ServerID, &r.Server, &r.Cluster, &r.Gateway, &r.Outbound, &cid, &rtt, &pending); err != nil {
			return err
		}
		r.CID, r.RTT, r.Pending = int64Ptr(cid), durPtr(rtt), int64Ptr(pending)
		f.Gateways = append(f.Gateways, r)
	}
	return rows.Err()
}

// leafCols are the leaf node columns both leaf reads select; leafWhere
// keeps the sites, not the server's own uplinks.
const (
	leafCols  = `l.epoch, l.server_id, s.server_name, coalesce(s.cluster, ''), l.lid, coalesce(l.name, ''), coalesce(l.account, ''), coalesce(l.ip, ''), l.rtt_ns, l.subscriptions, coalesce(l.compression, '')`
	leafWhere = `NOT coalesce(l.is_spoke, false)`
)

func scanLeaf(rows *sql.Rows) (int64, api.TopologyLeafRow, error) {
	var r api.TopologyLeafRow
	var epoch int64
	var rtt, subs sql.NullInt64
	err := rows.Scan(&epoch, &r.ServerID, &r.Server, &r.Cluster, &r.LID, &r.Name, &r.Account, &r.IP, &rtt, &subs, &r.Compression)
	r.RTT, r.Subscriptions = durPtr(rtt), int64Ptr(subs)
	return epoch, r, err
}

// topologyLeafs reads the epoch's leaf node sites.
func (p *Provider) topologyLeafs(ctx context.Context, f *api.TopologyFacts) error {
	rows, err := p.db().QueryContext(ctx, `SELECT `+leafCols+`
FROM leafnodes l JOIN servers s USING (server_id) WHERE l.epoch = ? AND `+leafWhere, f.Epoch)
	if err != nil {
		return fmt.Errorf("topology leaf nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		_, r, err := scanLeaf(rows)
		if err != nil {
			return err
		}
		f.Leafnodes = append(f.Leafnodes, r)
	}
	return rows.Err()
}
