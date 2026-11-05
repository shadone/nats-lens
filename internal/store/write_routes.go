package store

import (
	"fmt"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// routeRow is one row of routes without its epoch and server. Times are
// Unix microseconds of UTC, made into TIMESTAMP in SQL so that no session
// time zone shifts them; nil fields are NULL.
type routeRow struct {
	Rid           int64  `json:"rid"`
	RemoteID      string `json:"remote_id"`
	RemoteName    string `json:"remote_name"`
	DidSolicit    bool   `json:"did_solicit"`
	IsConfigured  bool   `json:"is_configured"`
	IP            string `json:"ip"`
	Port          int    `json:"port"`
	StartTime     any    `json:"start_time"`
	LastActivity  any    `json:"last_activity"`
	RTT           any    `json:"rtt_ns"`
	Pending       int    `json:"pending_bytes"`
	InMsgs        int64  `json:"in_msgs"`
	OutMsgs       int64  `json:"out_msgs"`
	InBytes       int64  `json:"in_bytes"`
	OutBytes      int64  `json:"out_bytes"`
	Subscriptions int64  `json:"subscriptions"`
	Account       string `json:"account"`
	Compression   string `json:"compression"`
}

const routeShape = `[{"rid":"BIGINT","remote_id":"VARCHAR","remote_name":"VARCHAR","did_solicit":"BOOLEAN",` +
	`"is_configured":"BOOLEAN","ip":"VARCHAR","port":"INTEGER","start_time":"BIGINT","last_activity":"BIGINT",` +
	`"rtt_ns":"BIGINT","pending_bytes":"BIGINT","in_msgs":"BIGINT","out_msgs":"BIGINT","in_bytes":"BIGINT",` +
	`"out_bytes":"BIGINT","subscriptions":"BIGINT","account":"VARCHAR","compression":"VARCHAR"}]`

// writeRoutes stores a server's ROUTEZ routes in batches handed to DuckDB
// as JSON.
func (w *writer) writeRoutes(v *collect.ServerView) {
	if v.Routez == nil {
		return
	}
	rows := make([]routeRow, 0, len(v.Routez.Routes))
	for _, r := range v.Routez.Routes {
		if r == nil {
			continue
		}
		rows = append(rows, routeRow{
			Rid: i64(r.Rid), RemoteID: r.RemoteID, RemoteName: r.RemoteName, DidSolicit: r.DidSolicit,
			IsConfigured: r.IsConfigured, IP: r.IP, Port: r.Port, StartTime: unixMicro(r.Start),
			LastActivity: unixMicro(r.LastActivity), RTT: durNS(r.RTT), Pending: r.Pending, InMsgs: r.InMsgs,
			OutMsgs: r.OutMsgs, InBytes: r.InBytes, OutBytes: r.OutBytes, Subscriptions: int64(r.NumSubs),
			Account: r.Account, Compression: r.Compression,
		})
	}
	q := fmt.Sprintf(`INSERT INTO routes (epoch, server_id, rid, remote_id, remote_name, did_solicit, is_configured,
    ip, port, start_time, last_activity, rtt_ns, pending_bytes, in_msgs, out_msgs, in_bytes, out_bytes,
    subscriptions, account, compression)
SELECT %d, ?::VARCHAR, rid, remote_id, remote_name, did_solicit, is_configured, ip, port,
    make_timestamp(start_time), make_timestamp(last_activity), rtt_ns, pending_bytes, in_msgs, out_msgs,
    in_bytes, out_bytes, subscriptions, account, compression
`+fromJSON(routeShape), w.epoch)
	writeJSON(w, "routes", q, rows, jsonBatch, v.Info.ID)
}

// gatewayRow is one row of gateways without its epoch and server. The
// connection fields are nil, so NULL, for a gateway without a connection.
type gatewayRow struct {
	Gateway      string `json:"gateway"`
	Direction    string `json:"direction"`
	IsConfigured bool   `json:"is_configured"`
	Cid          any    `json:"cid"`
	IP           any    `json:"ip"`
	Port         any    `json:"port"`
	RTT          any    `json:"rtt_ns"`
	Pending      any    `json:"pending_bytes"`
	InMsgs       any    `json:"in_msgs"`
	OutMsgs      any    `json:"out_msgs"`
	InBytes      any    `json:"in_bytes"`
	OutBytes     any    `json:"out_bytes"`
}

const gatewayShape = `[{"gateway":"VARCHAR","direction":"VARCHAR","is_configured":"BOOLEAN","cid":"BIGINT",` +
	`"ip":"VARCHAR","port":"INTEGER","rtt_ns":"BIGINT","pending_bytes":"BIGINT","in_msgs":"BIGINT",` +
	`"out_msgs":"BIGINT","in_bytes":"BIGINT","out_bytes":"BIGINT"}]`

func gatewayOf(name, dir string, g *server.RemoteGatewayz) gatewayRow {
	r := gatewayRow{Gateway: name, Direction: dir, IsConfigured: g.IsConfigured}
	if c := g.Connection; c != nil {
		r.Cid, r.IP, r.Port, r.RTT, r.Pending = i64(c.Cid), c.IP, c.Port, durNS(c.RTT), c.Pending
		r.InMsgs, r.OutMsgs, r.InBytes, r.OutBytes = c.InMsgs, c.OutMsgs, c.InBytes, c.OutBytes
	}
	return r
}

// writeGateways stores a server's GATEWAYZ gateways, outbound and then
// inbound, in batches handed to DuckDB as JSON.
func (w *writer) writeGateways(v *collect.ServerView) {
	if v.Gatewayz == nil {
		return
	}
	var rows []gatewayRow
	for _, name := range sortedKeys(v.Gatewayz.OutboundGateways) {
		if g := v.Gatewayz.OutboundGateways[name]; g != nil {
			rows = append(rows, gatewayOf(name, "outbound", g))
		}
	}
	for _, name := range sortedKeys(v.Gatewayz.InboundGateways) {
		for _, g := range v.Gatewayz.InboundGateways[name] {
			if g != nil {
				rows = append(rows, gatewayOf(name, "inbound", g))
			}
		}
	}
	q := fmt.Sprintf(`INSERT INTO gateways (epoch, server_id, gateway, direction, is_configured, cid, ip, port, rtt_ns,
    pending_bytes, in_msgs, out_msgs, in_bytes, out_bytes)
SELECT %d, ?::VARCHAR, gateway, direction, is_configured, cid, ip, port, rtt_ns, pending_bytes, in_msgs,
    out_msgs, in_bytes, out_bytes
`+fromJSON(gatewayShape), w.epoch)
	writeJSON(w, "gateways", q, rows, jsonBatch, v.Info.ID)
}
