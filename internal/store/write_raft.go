package store

import (
	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// raftPeerNames maps raft peer IDs to server names from everything the servers
// reported in the epoch: the meta leader and members, asset replicas and
// raft group peers. RAFTZ names leaders only by peer ID.
func raftPeerNames(snap *collect.Snapshot) map[string]string {
	out := map[string]string{}
	add := func(id, name string) {
		if id != "" && name != "" {
			out[id] = name
		}
	}
	addCluster := func(ci *server.ClusterInfo) {
		if ci == nil {
			return
		}
		for _, r := range ci.Replicas {
			if r != nil {
				add(r.Peer, r.Name)
			}
		}
	}
	for _, v := range snap.Servers {
		if j := v.Jsz; j != nil {
			if m := j.Meta; m != nil {
				add(m.Peer, m.Leader)
				for _, r := range m.Replicas {
					if r != nil {
						add(r.Peer, r.Name)
					}
				}
			}
			for _, a := range j.AccountDetails {
				if a == nil {
					continue
				}
				for _, s := range a.Streams {
					addCluster(s.Cluster)
					for _, c := range s.Consumer {
						if c != nil {
							addCluster(c.Cluster)
						}
					}
				}
			}
		}
		for _, groups := range v.Raftz {
			for _, g := range groups {
				for id, p := range g.Peers {
					add(id, p.Name)
				}
			}
		}
	}
	return out
}

var raftStatsCols = []string{
	"epoch", "server_id", "group_id", "raft_account", "state", "size", "quorum_needed", "observer", "paused",
	"overrun", "overrun_count", "committed", "applied", "catching_up", "leader", "leader_name", "leader_since",
	"ever_had_leader", "term", "voted_for", "pterm", "pindex",
	"ipq_proposal_len", "ipq_entry_len", "ipq_resp_len", "ipq_apply_len",
	"wal_msgs", "wal_bytes", "wal_first_seq", "wal_last_seq", "wal_error",
	"peers_total", "peers_known", "peers",
}

func (w *writer) writeRaft(v *collect.ServerView) {
	if w.peers == nil {
		w.peers = raftPeerNames(w.snap)
	}
	for _, acc := range sortedKeys(v.Raftz) {
		groups := v.Raftz[acc]
		for _, id := range sortedKeys(groups) {
			g := groups[id]
			w.ids.raftGroup(id)
			known := 0
			for _, p := range g.Peers {
				if p.Known {
					known++
				}
			}
			var walErr any
			if len(g.WALError) > 0 && string(g.WALError) != "null" {
				walErr = string(g.WALError)
			}
			var peers any
			if len(g.Peers) > 0 {
				peers = jsonText(g.Peers)
			}
			w.insert("raft_stats", raftStatsCols, w.epoch, v.Info.ID, id, acc, g.State, g.Size, g.QuorumNeeded,
				g.Observer, g.Paused, g.Overrun, i64(g.OverrunCount), i64(g.Committed), i64(g.Applied),
				g.CatchingUp, g.Leader, nullIfEmpty(w.peers[g.Leader]), tsp(g.LeaderSince), g.EverHadLeader, i64(g.Term), g.Vote,
				i64(g.PTerm), i64(g.PIndex), g.IPQPropLen, g.IPQEntryLen, g.IPQRespLen, g.IPQApplyLen,
				i64(g.WAL.Msgs), i64(g.WAL.Bytes), i64(g.WAL.FirstSeq), i64(g.WAL.LastSeq), walErr,
				len(g.Peers), known, peers)
		}
	}
}
