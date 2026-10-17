package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// SysSubjects returns the system account subjects captured. The system
// account also receives the JetStream advisories that concern no account:
// meta leader elected, server out of storage, server removed and API limit
// reached; those are decoded by DecodeJS. Account connection counts are
// sent by every server for every account every 30 seconds, so they are
// captured only when accountConns is true.
func SysSubjects(accountConns bool) []string {
	out := []string{
		"$SYS.ACCOUNT.*.CONNECT",
		"$SYS.ACCOUNT.*.DISCONNECT",
		"$SYS.SERVER.*.LAMEDUCK",
		"$SYS.SERVER.*.SHUTDOWN",
		"$SYS.SERVER.*.CLIENT.AUTH.ERR",
		authErrAccountSubject,
		"$JS.EVENT.ADVISORY.>",
	}
	if accountConns {
		out = append(out, "$SYS.ACCOUNT.*.SERVER.CONNS")
	}
	return out
}

// authErrAccountSubject carries authentication errors scoped to an account
// (auth callout); it is published in that account, so the account
// connections subscribe to it too.
const authErrAccountSubject = "$SYS.ACCOUNT.CLIENT.AUTH.ERR"

// DecodeSys turns a message on a system account subject into an event.
func DecodeSys(subject string, data []byte, received time.Time) (Event, error) {
	if strings.HasPrefix(subject, "$JS.EVENT.") {
		return DecodeJS("", subject, data, received)
	}
	toks := strings.Split(subject, ".")
	raw, err := cleanRaw(data)
	if err != nil {
		return Event{}, fmt.Errorf("%s: %w", subject, err)
	}
	ev := Event{ReceivedAt: received.UTC(), Source: SourceSys, Raw: raw}
	switch {
	case len(toks) == 4 && toks[1] == "ACCOUNT" && toks[3] == "CONNECT":
		var m server.ConnectEventMsg
		if err := json.Unmarshal(data, &m); err != nil {
			return Event{}, fmt.Errorf("%s: %w", subject, err)
		}
		ev.Kind, ev.Type = "client.connect", m.Type
		ev.setTime(m.Time)
		ev.setServer(m.Server)
		ev.setClient(&m.Client, toks[2])
		ev.setAccountName(&m.Client)
		ev.Summary = fmt.Sprintf("%s connected to %s from %s", clientLabel(&m.Client), orDash(ev.ServerName), orDash(ev.ClientHost))
	case len(toks) == 4 && toks[1] == "ACCOUNT" && toks[3] == "DISCONNECT":
		m, err := decodeDisconnect(data)
		if err != nil {
			return Event{}, fmt.Errorf("%s: %w", subject, err)
		}
		ev.Kind, ev.Type, ev.Reason = "client.disconnect", m.Type, m.Reason
		ev.setTime(m.Time)
		ev.setServer(m.Server)
		ev.setClient(&m.Client, toks[2])
		ev.setAccountName(&m.Client)
		ev.Summary = fmt.Sprintf("%s disconnected from %s: %s", clientLabel(&m.Client), orDash(ev.ServerName), orDash(m.Reason))
	case subject == authErrAccountSubject,
		len(toks) == 6 && toks[1] == "SERVER" && toks[3] == "CLIENT" && toks[4] == "AUTH" && toks[5] == "ERR":
		m, err := decodeDisconnect(data)
		if err != nil {
			return Event{}, fmt.Errorf("%s: %w", subject, err)
		}
		// The server-wide and the account-scoped event of one failure have
		// different IDs; the server, client ID and start time identify it.
		ev.id = fmt.Sprintf("auth:%s:%d:%v", m.Server.ID, m.Client.ID, m.Client.Start)
		ev.Kind, ev.Type, ev.Reason = "client.auth_error", m.Type, m.Reason
		ev.setTime(m.Time)
		ev.setServer(m.Server)
		ev.setClient(&m.Client, "")
		ev.Summary = fmt.Sprintf("authentication failed on %s for user %s from %s: %s",
			orDash(ev.ServerName), orDash(ev.ClientUser), orDash(ev.ClientHost), orDash(m.Reason))
	case len(toks) == 5 && toks[1] == "ACCOUNT" && toks[3] == "SERVER" && toks[4] == "CONNS":
		var m server.AccountNumConns
		if err := json.Unmarshal(data, &m); err != nil {
			return Event{}, fmt.Errorf("%s: %w", subject, err)
		}
		ev.Kind, ev.Type = "account.connections", m.Type
		ev.setTime(m.Time)
		ev.setServer(m.Server)
		ev.Account = firstNonEmpty(m.Account, toks[2])
		ev.Summary = fmt.Sprintf("account %s has %d connections on %s, %d in total, %d slow consumers",
			ev.Account, m.Conns, orDash(ev.ServerName), m.TotalConns, m.SlowConsumers)
	case len(toks) == 4 && toks[1] == "SERVER" && (toks[3] == "LAMEDUCK" || toks[3] == "SHUTDOWN"):
		var si server.ServerInfo
		if err := json.Unmarshal(data, &si); err != nil {
			return Event{}, fmt.Errorf("%s: %w", subject, err)
		}
		ev.setTime(si.Time)
		ev.setServer(si)
		if toks[3] == "LAMEDUCK" {
			ev.Kind = "server.lameduck"
			ev.Summary = fmt.Sprintf("server %s entered lame duck mode", orDash(ev.ServerName))
		} else {
			ev.Kind = "server.shutdown"
			ev.Summary = fmt.Sprintf("server %s shut down", orDash(ev.ServerName))
		}
	default:
		return Event{}, fmt.Errorf("unexpected system subject %s", subject)
	}
	return ev, nil
}

// decodeDisconnect reads a disconnect or authentication error payload; both
// use the disconnect message type.
func decodeDisconnect(data []byte) (server.DisconnectEventMsg, error) {
	var m server.DisconnectEventMsg
	err := json.Unmarshal(data, &m)
	return m, err
}

// setTime uses the payload's time when it has one.
func (e *Event) setTime(t time.Time) {
	if t.IsZero() {
		e.Time = e.ReceivedAt
		return
	}
	e.Time = t.UTC()
}

func (e *Event) setServer(si server.ServerInfo) {
	e.ServerName = firstNonEmpty(si.Name, si.ID)
}

// setClient copies the client fields. An account already set (the account
// the message was received in) is kept; otherwise the client's account or
// the subject's account token is used.
func (e *Event) setClient(c *server.ClientInfo, subjectAccount string) {
	if c == nil {
		return
	}
	e.Account = firstNonEmpty(e.Account, c.Account, subjectAccount)
	e.ClientName = c.Name
	e.clientID = c.ID
	e.ClientHost = c.Host
	e.ClientUser = c.User
	e.ClientLang = c.Lang
	if c.Lang != "" && c.Version != "" {
		e.ClientLang = c.Lang + " " + c.Version
	}
}

// setAccountName takes the account's readable name from a connect or
// disconnect event, where the server puts the account's name tag in the
// client's name_tag. Other payloads (API audits, snapshot and restore
// advisories) put the user's name tag there, so only these two decoders
// call it.
func (e *Event) setAccountName(c *server.ClientInfo) {
	if c != nil && c.NameTag != "" && c.Account == e.Account {
		e.AccountName = c.NameTag
	}
}

// clientLabel names a client by its connection name, or by its ID when it
// has none.
func clientLabel(c *server.ClientInfo) string {
	label := "client " + c.Name
	if c.Name == "" {
		label = fmt.Sprintf("client cid %d", c.ID)
	}
	if c.User != "" {
		label += " (user " + c.User + ")"
	}
	return label
}

// cleanRaw returns the payload as compact JSON with every user JWT removed:
// any object key named jwt, in any letter case and at any depth. A JWT is a
// credential, and the other fields already say who connected. Numbers keep
// their exact text.
func cleanRaw(data []byte) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil {
		return nil, fmt.Errorf("payload is not a JSON object: %w", err)
	}
	return json.Marshal(withoutJWT(top))
}

// withoutJWT returns v with every object key named jwt left out; it builds
// new maps and slices rather than changing v.
func withoutJWT(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if strings.EqualFold(k, "jwt") {
				continue
			}
			out[k] = withoutJWT(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withoutJWT(e)
		}
		return out
	default:
		return v
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
