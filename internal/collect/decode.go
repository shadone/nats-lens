package collect

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats-server/v2/server"
)

// reply is the envelope every $SYS.REQ.SERVER.PING.<X> answer uses.
type reply[T any] struct {
	Server *server.ServerInfo `json:"server"`
	Data   T                  `json:"data"`
	Error  *server.ApiError   `json:"error"`
}

// RaftGroup is server.RaftzGroup with the WAL error kept as raw JSON.
// The server type declares WALError as a Go error interface, which
// encoding/json cannot decode into; the shallower field here wins.
type RaftGroup struct {
	server.RaftzGroup
	WALError json.RawMessage `json:"wal_error,omitempty"`
}

// RaftzStatus maps account name to raft group id to group state.
type RaftzStatus map[string]map[string]RaftGroup

// ErrNoServerInfo marks a reply that does not say which server sent it.
var ErrNoServerInfo = errors.New("reply has no server info")

// decodeReply decodes one enveloped reply. A reply that carries an API
// error returns the server info with the error, so the caller still knows
// which server answered.
func decodeReply[T any](b []byte) (server.ServerInfo, T, error) {
	var r reply[T]
	var zero T
	if err := json.Unmarshal(b, &r); err != nil {
		return server.ServerInfo{}, zero, fmt.Errorf("decode reply: %w", err)
	}
	if r.Server == nil || r.Server.ID == "" {
		return server.ServerInfo{}, zero, ErrNoServerInfo
	}
	if r.Error != nil {
		return *r.Server, zero, fmt.Errorf("server error %d: %s", r.Error.Code, r.Error.Description)
	}
	return *r.Server, r.Data, nil
}

// decodeStatsz decodes a STATSZ reply, which is not enveloped.
func decodeStatsz(b []byte) (server.ServerInfo, *server.ServerStats, error) {
	var m server.ServerStatsMsg
	if err := json.Unmarshal(b, &m); err != nil {
		return server.ServerInfo{}, nil, fmt.Errorf("decode statsz: %w", err)
	}
	if m.Server.ID == "" {
		return server.ServerInfo{}, nil, ErrNoServerInfo
	}
	return m.Server, &m.Stats, nil
}

// Apply decodes a reply for e and stores it on the matching server view,
// creating the view if this is the first reply from that server.
func (snap *Snapshot) Apply(e Endpoint, b []byte) error {
	orig := e
	var (
		info server.ServerInfo
		err  error
		set  func(v *ServerView)
	)
	if _, ok := e.raftzAccount(); ok {
		e = Raftz
	}
	switch e {
	case Statsz:
		var st *server.ServerStats
		info, st, err = decodeStatsz(b)
		set = func(v *ServerView) { v.Statsz = st }
	case Varz:
		var d *server.Varz
		info, d, err = decodeReply[*server.Varz](b)
		set = func(v *ServerView) { v.Varz = d }
	case Connz:
		var d *server.Connz
		info, d, err = decodeReply[*server.Connz](b)
		jwts := takeUserJWTs(d)
		set = func(v *ServerView) { v.Connz, v.UserJWTs = d, jwts }
	case Routez:
		var d *server.Routez
		info, d, err = decodeReply[*server.Routez](b)
		set = func(v *ServerView) { v.Routez = d }
	case Gatewayz:
		var d *server.Gatewayz
		info, d, err = decodeReply[*server.Gatewayz](b)
		set = func(v *ServerView) { v.Gatewayz = d }
	case Leafz:
		var d *server.Leafz
		info, d, err = decodeReply[*server.Leafz](b)
		set = func(v *ServerView) { v.Leafz = d }
	case Accountz:
		var d *server.Accountz
		info, d, err = decodeReply[*server.Accountz](b)
		set = func(v *ServerView) { v.Accountz = d }
	case AccountStatz:
		var d *server.AccountStatz
		info, d, err = decodeReply[*server.AccountStatz](b)
		set = func(v *ServerView) { v.AccountStatz = d }
	case Jsz:
		var d *server.JSInfo
		info, d, err = decodeReply[*server.JSInfo](b)
		set = func(v *ServerView) { v.Jsz = d }
	case Healthz:
		var d *server.HealthStatus
		info, d, err = decodeReply[*server.HealthStatus](b)
		set = func(v *ServerView) { v.Healthz = d }
	case Raftz:
		var d RaftzStatus
		info, d, err = decodeReply[RaftzStatus](b)
		set = func(v *ServerView) { v.Raftz = mergeRaftz(v.Raftz, d) }
	case Ipqueuesz:
		var d server.IpqueueszStatus
		info, d, err = decodeReply[server.IpqueueszStatus](b)
		set = func(v *ServerView) { v.Ipqueuesz = d }
	default:
		return fmt.Errorf("unknown endpoint %s", e)
	}
	if info.ID == "" {
		return err
	}
	v := snap.view(info)
	if err != nil {
		v.Status[orig] = EndpointStatus{State: StateError, Error: err.Error()}
		return nil
	}
	set(v)
	v.Status[orig] = EndpointStatus{State: StateOK}
	return nil
}

// mergeRaftz adds the accounts of b to a; the system account reply and the
// per-account replies cover different accounts.
func mergeRaftz(a, b RaftzStatus) RaftzStatus {
	if a == nil {
		a = RaftzStatus{}
	}
	for acc, groups := range b {
		a[acc] = groups
	}
	return a
}
