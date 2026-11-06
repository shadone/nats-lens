package checks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// prepareMetaGroups scopes the two meta checks without changing the public
// system finding identity. Membership overlap, not cluster/domain labels or
// the reused _meta_ ID, establishes a physical group. Only the evaluation
// window is read; evaluating history never borrows a future membership.
func prepareMetaGroups(ctx context.Context, conn *sql.Conn, lo, hi int64) error {
	rows, err := conn.QueryContext(ctx, `SELECT st.server_id, s.server_name, st.meta_leader, r.peers::VARCHAR
 FROM server_stats st JOIN servers s USING (server_id)
 LEFT JOIN raft_stats r ON r.epoch = st.epoch AND r.server_id = st.server_id AND r.group_id = '_meta_'
 WHERE st.epoch BETWEEN ? AND ? AND st.js_enabled`, lo, hi)
	if err != nil {
		return err
	}
	type observation struct {
		id, name, leader string
		peers            sql.NullString
	}
	var obs []observation
	names := map[string]map[string]bool{}
	parent := map[string]string{}
	var root func(string) string
	root = func(x string) string {
		p, ok := parent[x]
		if !ok {
			parent[x] = x
			return x
		}
		if p != x {
			parent[x] = root(p)
		}
		return parent[x]
	}
	join := func(a, b string) {
		a, b = root(a), root(b)
		if a > b {
			a, b = b, a
		}
		parent[b] = a
	}
	for rows.Next() {
		var o observation
		var leader sql.NullString
		if err := rows.Scan(&o.id, &o.name, &leader, &o.peers); err != nil {
			rows.Close()
			return err
		}
		o.leader = leader.String
		obs = append(obs, o)
		if names[o.name] == nil {
			names[o.name] = map[string]bool{}
		}
		names[o.name][o.id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	proven := map[string]bool{}
	for _, o := range obs {
		if !o.peers.Valid {
			continue
		}
		// ASVS 1.5.2: decode only peer names into a fixed data type.
		var peers map[string]struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(o.peers.String), &peers); err != nil {
			return fmt.Errorf("decode meta membership: %w", err)
		}
		if len(peers) == 0 {
			continue
		}
		proven[o.id] = true
		for peer, p := range peers {
			join("server:"+o.id, "peer:"+peer)
			// A name can resolve a reporter only when it is unambiguous in the
			// bounded window; duplicate names never join otherwise separate groups.
			if len(names[p.Name]) == 1 {
				for id := range names[p.Name] {
					join("peer:"+peer, "server:"+id)
					proven[id] = true
				}
			}
		}
	}
	// JSZ can attach a reporter without RAFTZ to a uniquely identified leader.
	// It cannot create a separate group merely from a different domain label.
	candidates := map[string]map[string]bool{}
	for _, o := range obs {
		if proven[o.id] || len(names[o.leader]) != 1 {
			continue
		}
		for id := range names[o.leader] {
			if proven[id] {
				if candidates[o.id] == nil {
					candidates[o.id] = map[string]bool{}
				}
				candidates[o.id][root("server:"+id)] = true
			}
		}
	}
	for id, groups := range candidates {
		if len(groups) == 1 {
			for group := range groups {
				join("server:"+id, group)
				proven[id] = true
			}
		}
	}
	members := map[string][]string{}
	ids := map[string]string{}
	for name, servers := range names {
		for id := range servers {
			key := ""
			if proven[id] {
				key = root("server:" + id)
			}
			ids[id] = key
			members[key] = append(members[key], name)
		}
	}
	labels := map[string]string{}
	for key, ns := range members {
		sort.Strings(ns)
		if key == "" {
			labels[key] = "Unscoped observations (meta group identity unknown)"
		} else {
			labels[key] = "Meta group [" + strings.Join(ns, ", ") + "]"
		}
	}
	type mapping struct {
		ID    string `json:"id"`
		Group string `json:"group_key"`
		Label string `json:"label"`
	}
	data := []mapping{}
	for id, key := range ids {
		data = append(data, mapping{id, key, labels[key]})
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// ASVS 1.2.4: stored names and membership stay bound data, never SQL text.
	_, err = conn.ExecContext(ctx, `CREATE OR REPLACE TEMP TABLE meta_check_groups AS
 SELECT x.id AS server_id, x.group_key, x.label FROM
 (SELECT unnest(from_json(?::JSON, '[{"id":"VARCHAR","group_key":"VARCHAR","label":"VARCHAR"}]')) AS x)`, string(encoded))
	return err
}
