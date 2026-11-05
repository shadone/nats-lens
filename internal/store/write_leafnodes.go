package store

import (
	"fmt"

	"github.com/shadone/nats-lens/internal/collect"
)

// leafnodeRow is one row of leafnodes without its epoch and server; a nil
// field is NULL.
type leafnodeRow struct {
	Lid           int64  `json:"lid"`
	Name          string `json:"name"`
	IsSpoke       bool   `json:"is_spoke"`
	Account       string `json:"account"`
	IP            string `json:"ip"`
	Port          int    `json:"port"`
	RTT           any    `json:"rtt_ns"`
	InMsgs        int64  `json:"in_msgs"`
	OutMsgs       int64  `json:"out_msgs"`
	InBytes       int64  `json:"in_bytes"`
	OutBytes      int64  `json:"out_bytes"`
	Subscriptions int64  `json:"subscriptions"`
	Compression   string `json:"compression"`
}

const leafnodeShape = `[{"lid":"BIGINT","name":"VARCHAR","is_spoke":"BOOLEAN","account":"VARCHAR","ip":"VARCHAR",` +
	`"port":"INTEGER","rtt_ns":"BIGINT","in_msgs":"BIGINT","out_msgs":"BIGINT","in_bytes":"BIGINT",` +
	`"out_bytes":"BIGINT","subscriptions":"BIGINT","compression":"VARCHAR"}]`

// writeLeafnodes stores a server's LEAFZ leaf nodes in batches handed to
// DuckDB as JSON; one INSERT per row took about a third of a millisecond
// each (TestLeafnodeWriteTime).
func (w *writer) writeLeafnodes(v *collect.ServerView) {
	if v.Leafz == nil {
		return
	}
	rows := make([]leafnodeRow, 0, len(v.Leafz.Leafs))
	for _, l := range v.Leafz.Leafs {
		if l == nil {
			continue
		}
		rows = append(rows, leafnodeRow{
			Lid: i64(l.ID), Name: l.Name, IsSpoke: l.IsSpoke, Account: l.Account, IP: l.IP, Port: l.Port,
			RTT: durNS(l.RTT), InMsgs: l.InMsgs, OutMsgs: l.OutMsgs, InBytes: l.InBytes, OutBytes: l.OutBytes,
			Subscriptions: int64(l.NumSubs), Compression: l.Compression,
		})
	}
	q := fmt.Sprintf(`INSERT INTO leafnodes (epoch, server_id, lid, name, is_spoke, account, ip, port, rtt_ns,
    in_msgs, out_msgs, in_bytes, out_bytes, subscriptions, compression)
SELECT %d, ?::VARCHAR, lid, name, is_spoke, account, ip, port, rtt_ns, in_msgs, out_msgs, in_bytes, out_bytes,
    subscriptions, compression
`+fromJSON(leafnodeShape), w.epoch)
	writeJSON(w, "leafnodes", q, rows, jsonBatch, v.Info.ID)
}
