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
// system finding identity. Peer names resolving to observed processes, not
// cluster/domain labels or unresolved name hashes, establish membership. Only the evaluation
// window is read; evaluating history never borrows a future membership.
func prepareMetaGroups(ctx context.Context, conn *sql.Conn, lo, hi int64) error {
	rows, err := conn.QueryContext(ctx, `SELECT st.epoch, st.server_id, s.server_name, st.meta_leader, r.peers::VARCHAR
 FROM server_stats st JOIN servers s USING (server_id)
 LEFT JOIN raft_stats r ON r.epoch = st.epoch AND r.server_id = st.server_id AND r.group_id = '_meta_'
 WHERE st.epoch BETWEEN ? AND ? AND st.js_enabled`, lo, hi)
	if err != nil {
		return err
	}
	type observation struct {
		epoch            int64
		id, name, leader string
		peers            sql.NullString
	}
	var obs []observation
	names := map[string]map[string]bool{}
	epochNames := map[int64]map[string]string{}
	ambiguousNames := map[string]bool{}
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
		if err := rows.Scan(&o.epoch, &o.id, &o.name, &leader, &o.peers); err != nil {
			rows.Close()
			return err
		}
		o.leader = leader.String
		obs = append(obs, o)
		if epochNames[o.epoch] == nil {
			epochNames[o.epoch] = map[string]string{}
		}
		if id, ok := epochNames[o.epoch][o.name]; ok && id != o.id {
			ambiguousNames[o.name] = true
		}
		epochNames[o.epoch][o.name] = o.id
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
	type peerInfo struct {
		Name string `json:"name"`
	}
	// ASVS 1.5.2: decode only peer names into a fixed data type. Cache the
	// repeated configuration so work scales with membership changes.
	configs := map[string]map[string]peerInfo{}
	peerNames := map[string]string{}
	ambiguousPeers := map[string]bool{}
	for _, o := range obs {
		if !o.peers.Valid {
			continue
		}
		if _, ok := configs[o.peers.String]; ok {
			continue
		}
		var peers map[string]peerInfo
		if err := json.Unmarshal([]byte(o.peers.String), &peers); err != nil {
			return fmt.Errorf("decode meta membership: %w", err)
		}
		configs[o.peers.String] = peers
		for peer, p := range peers {
			if name, ok := peerNames[peer]; ok && name != p.Name {
				ambiguousPeers[peer] = true
			}
			peerNames[peer] = p.Name
		}
	}
	membershipObserved := map[string]bool{}
	for _, o := range obs {
		if len(configs[o.peers.String]) > 0 {
			membershipObserved[o.id] = true
		}
	}
	proven := map[string]bool{}
	unresolved := map[string]map[string]bool{}
	for _, o := range obs {
		for peer, p := range configs[o.peers.String] {
			// A peer hash is derived from a name, not a physical process ID.
			// Conflicting mappings and duplicate responding names are unusable.
			if peer == "" || ambiguousPeers[peer] || ambiguousNames[p.Name] {
				continue
			}
			if p.Name == "" || len(names[p.Name]) != 1 {
				if unresolved[peer] == nil {
					unresolved[peer] = map[string]bool{}
				}
				unresolved[peer][o.id] = true
				continue
			}
			for id := range names[p.Name] {
				join("server:"+o.id, "server:"+id)
				proven[o.id], proven[id] = true, true
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
	// An unresolved peer shared by otherwise separate components could be one
	// down member or multiple down processes reusing a name (or hash). Neither
	// merging nor declaring those components independent is justified. Mark
	// every touched component uncertain; this also covers chains of bridges.
	uncertain := map[string]bool{}
	for _, reporters := range unresolved {
		groups := map[string]bool{}
		for id := range reporters {
			groups[root("server:"+id)] = true
		}
		if len(groups) > 1 {
			for group := range groups {
				uncertain[group] = true
			}
		}
	}
	members := map[string][]string{}
	ids := map[string]string{}
	for name, servers := range names {
		for id := range servers {
			key := ""
			if proven[id] && !uncertain[root("server:"+id)] {
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
		ID                 string `json:"id"`
		Group              string `json:"group_key"`
		Label              string `json:"label"`
		MembershipObserved bool   `json:"membership_observed"`
	}
	data := []mapping{}
	for id, key := range ids {
		data = append(data, mapping{id, key, labels[key], membershipObserved[id]})
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// ASVS 1.2.4: stored names and membership stay bound data, never SQL text.
	_, err = conn.ExecContext(ctx, `CREATE OR REPLACE TEMP TABLE meta_check_groups AS
 SELECT x.id AS server_id, x.group_key, x.label, x.membership_observed FROM
 (SELECT unnest(from_json(?::JSON, '[{"id":"VARCHAR","group_key":"VARCHAR","label":"VARCHAR","membership_observed":"BOOLEAN"}]')) AS x)`, string(encoded))
	return err
}
