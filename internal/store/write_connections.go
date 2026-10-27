package store

import (
	"fmt"

	"github.com/shadone/nats-lens/internal/collect"
)

// connectionRow is one row of connections without its epoch and server.
// Times are Unix microseconds of UTC, made into TIMESTAMP in SQL so that
// no session time zone shifts them; nil fields are NULL.
type connectionRow struct {
	Cid            int64  `json:"cid"`
	Kind           string `json:"kind"`
	Type           string `json:"type"`
	Name           string `json:"name"`
	Lang           string `json:"lang"`
	Version        string `json:"version"`
	Account        string `json:"account"`
	AuthorizedUser string `json:"authorized_user"`
	IP             string `json:"ip"`
	Port           int    `json:"port"`
	StartTime      any    `json:"start_time"`
	LastActivity   any    `json:"last_activity"`
	RTT            any    `json:"rtt_ns"`
	Pending        int    `json:"pending_bytes"`
	InMsgs         int64  `json:"in_msgs"`
	OutMsgs        int64  `json:"out_msgs"`
	InBytes        int64  `json:"in_bytes"`
	OutBytes       int64  `json:"out_bytes"`
	Stalls         int64  `json:"stalls"`
	Subscriptions  int64  `json:"subscriptions"`
	TLSVersion     string `json:"tls_version"`
	NameTag        string `json:"name_tag"`
	UserJWT        bool   `json:"user_jwt"`
	JWTExpires     any    `json:"jwt_expires"`
}

const connectionShape = `[{"cid":"BIGINT","kind":"VARCHAR","type":"VARCHAR","name":"VARCHAR","lang":"VARCHAR",` +
	`"version":"VARCHAR","account":"VARCHAR","authorized_user":"VARCHAR","ip":"VARCHAR","port":"INTEGER",` +
	`"start_time":"BIGINT","last_activity":"BIGINT","rtt_ns":"BIGINT","pending_bytes":"BIGINT",` +
	`"in_msgs":"BIGINT","out_msgs":"BIGINT","in_bytes":"BIGINT","out_bytes":"BIGINT","stalls":"BIGINT",` +
	`"subscriptions":"BIGINT","tls_version":"VARCHAR","name_tag":"VARCHAR","user_jwt":"BOOLEAN","jwt_expires":"BIGINT"}]`

// connectionBatch is the most rows one INSERT carries: about 2.5 MB of
// JSON, as a row is about 500 bytes.
const connectionBatch = 5000

// writeConnections stores a server's CONNZ connections in batches handed
// to DuckDB as JSON; one INSERT per row took about a third of a
// millisecond each.
func (w *writer) writeConnections(v *collect.ServerView) {
	if v.Connz == nil {
		return
	}
	rows := make([]connectionRow, 0, len(v.Connz.Conns))
	for _, c := range v.Connz.Conns {
		if c == nil {
			continue
		}
		if c.NameTag != "" {
			// CONNZ gives the account's name tag with each connection.
			w.ids.account(c.Account, c.NameTag, false)
		}
		j, hasJWT := v.UserJWTs[c.Cid]
		rows = append(rows, connectionRow{
			Cid: i64(c.Cid), Kind: c.Kind, Type: c.Type, Name: c.Name, Lang: c.Lang, Version: c.Version,
			Account: c.Account, AuthorizedUser: c.AuthorizedUser, IP: c.IP, Port: c.Port,
			StartTime: unixMicro(c.Start), LastActivity: unixMicro(c.LastActivity), RTT: durNS(c.RTT),
			Pending: c.Pending, InMsgs: c.InMsgs, OutMsgs: c.OutMsgs, InBytes: c.InBytes, OutBytes: c.OutBytes,
			Stalls: c.Stalls, Subscriptions: int64(c.NumSubs), TLSVersion: c.TLSVersion, NameTag: c.NameTag,
			UserJWT: hasJWT, JWTExpires: unixMicro(j.Expires),
		})
	}
	q := fmt.Sprintf(`INSERT INTO connections (epoch, server_id, cid, kind, type, name, lang, version, account,
    authorized_user, ip, port, start_time, last_activity, rtt_ns, pending_bytes, in_msgs, out_msgs, in_bytes,
    out_bytes, stalls, subscriptions, tls_version, name_tag, user_jwt, jwt_expires)
SELECT %d, %s, cid, kind, type, name, lang, version, account, authorized_user, ip, port,
    make_timestamp(start_time), make_timestamp(last_activity), rtt_ns, pending_bytes, in_msgs, out_msgs,
    in_bytes, out_bytes, stalls, subscriptions, tls_version, name_tag, user_jwt, make_timestamp(jwt_expires)
`+fromJSON(connectionShape), w.epoch, sqlString(v.Info.ID))
	writeJSON(w, "connections", q, rows, connectionBatch)
}
