package events

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats-server/v2/server/certidp"
)

const ocspSubject = "$SYS.SERVER.S0.OCSP.PEER.CONN.REJECT"

// Synthetic payload built from the pinned v2.15.0 events.go schema and
// sendOCSPPeerRejectEvent, including the certificate bytes it publishes.
func ocspPayload(t *testing.T) []byte {
	return marshal(t, server.OCSPPeerRejectEventMsg{
		TypedEvent: typed(server.OCSPPeerRejectEventMsgType), Kind: "Client",
		Server: server.ServerInfo{ID: "S0", Name: "nats-0"}, Reason: "client not OCSP valid",
		Peer: certidp.CertInfo{Subject: "private identity", Issuer: "private issuer", Fingerprint: "abc123", Raw: []byte("certificate bytes")},
	})
}

func TestOCSPDecode(t *testing.T) {
	if !slices.Contains(SysSubjects(false), ocspSubjectPattern) {
		t.Error("missing OCSP subscription")
	}
	data := ocspPayload(t)
	ev, err := DecodeSys(ocspSubject, data, recv)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != "server.ocsp_peer_rejected" || ev.Source != SourceSys || ev.Type != server.OCSPPeerRejectEventMsgType || ev.ServerName != "nats-0" || ev.Account != "" || ev.id != "x" || !ev.Time.Equal(recv.Add(-time.Second)) || ev.Reason != "client not OCSP valid" {
		t.Fatalf("event: %+v", ev)
	}
	if ev.ClientName != "" || ev.ClientUser != "" || !strings.Contains(ev.Summary, "Client") {
		t.Fatalf("peer became client: %+v", ev)
	}
	for _, forbidden := range []string{"private identity", "private issuer", "Y2VydGlmaWNhdGUgYnl0ZXM=", `"raw"`} {
		if strings.Contains(string(ev.Raw), forbidden) {
			t.Errorf("retained certificate identity: %s", ev.Raw)
		}
	}
	if !strings.Contains(string(ev.Raw), `"fingerprint":"abc123"`) || !strings.Contains(string(ev.Raw), `"id":"S0"`) {
		t.Errorf("lost safe identity: %s", ev.Raw)
	}
}

// Pattern literal in the test lets the RED stage compile before implementation.
const ocspSubjectPattern = "$SYS.SERVER.*.OCSP.PEER.CONN.REJECT"

func TestOCSPDecodeBoundaries(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(ocspPayload(t), &m); err != nil {
		t.Fatal(err)
	}
	m["future"] = map[string]any{"number": 17}
	m["peer"].(map[string]any)["future_certificate"] = "private"
	for _, stamp := range []string{"", "0001-01-01T00:00:00Z", "9999-01-01T00:00:00Z"} {
		m["timestamp"] = stamp
		if stamp == "" {
			delete(m, "timestamp")
		}
		ev, err := DecodeSys(ocspSubject, marshal(t, m), recv)
		if err != nil {
			t.Fatal(err)
		}
		if !ev.Time.Equal(recv) || strings.Contains(string(ev.Raw), `"future"`) || strings.Contains(string(ev.Raw), "future_certificate") {
			t.Fatalf("time/raw: %+v", ev)
		}
	}
	m["server"] = map[string]any{}
	ev, err := DecodeSys(ocspSubject, marshal(t, m), recv)
	if err != nil || ev.ServerName != "S0" {
		t.Fatalf("subject ID fallback: %+v %v", ev, err)
	}
	for _, subject := range []string{"$SYS.SERVER.S0.OCSP.PEER.LINK.INVALID", "$SYS.SERVER.S0.OCSP.PEER.CONN.REJECT.extra", "other.SERVER.S0.OCSP.PEER.CONN.REJECT"} {
		if _, err := DecodeSys(subject, ocspPayload(t), recv); err == nil {
			t.Errorf("accepted %s", subject)
		}
	}
	m["type"] = server.OCSPPeerChainlinkInvalidEventMsgType
	if _, err := DecodeSys(ocspSubject, marshal(t, m), recv); err == nil {
		t.Error("accepted chain status instead of rejection")
	}
	m["type"] = server.OCSPPeerRejectEventMsgType
	m["server"] = map[string]any{"id": "S1", "name": "other"}
	if _, err := DecodeSys(ocspSubject, marshal(t, m), recv); err == nil {
		t.Error("accepted mismatched server ID")
	}
}

func TestOCSPCapturedOnce(t *testing.T) {
	c := New(Options{Writer: &flakyWriter{}, Log: quiet()})
	cor, seen := newCorrelator(), map[string]time.Time{}
	in := incoming{sys: true, subject: ocspSubject, data: ocspPayload(t), at: recv}
	first, second := c.handle(cor, seen, in), c.handle(cor, seen, in)
	if len(first) != 1 || first[0].Kind != "server.ocsp_peer_rejected" || len(second) != 0 {
		t.Fatalf("first=%v second=%v", first, second)
	}
}

// Exercise encoding/json's accepted aliases and duplicate-key merging before
// persistence. No input object, including shadowed aliases, may survive whole.
func TestOCSPRawAllowlist(t *testing.T) {
	var base map[string]json.RawMessage
	if err := json.Unmarshal(ocspPayload(t), &base); err != nil {
		t.Fatal(err)
	}
	peer := string(base["peer"])
	delete(base, "peer")
	envelope := strings.TrimSuffix(string(marshal(t, base)), "}")
	cases := []struct{ name, fields, fingerprint string }{
		{"lower", `"peer":` + peer, "abc123"},
		{"title", `"Peer":` + peer, "abc123"},
		{"upper", `"PEER":` + peer, "abc123"},
		{"mixed", `"pEeR":` + peer, "abc123"},
		{"duplicates", `"Peer":` + peer + `,"peer":{"fingerprint":"last"},"PEER":{"subject":"private alias"}`, "last"},
		{"reverse", `"peer":{"fingerprint":"first"},"PEER":` + peer + `,"Peer":{"issuer":"private alias"}`, "abc123"},
		{"same key", `"Peer":` + peer + `,"Peer":{"fingerprint":"last"}`, "last"},
		{"future nested", `"pEeR":` + strings.TrimSuffix(peer, "}") + `,"future":{"chain":[{"raw":"private nested","subject":"private nested"}]}}`, "abc123"},
		{"empty", `"PEER":{"subject":"private empty","issuer":"private empty","raw":"Y2VydA=="}`, ""},
		{"null after alias", `"Peer":` + peer + `,"peer":null`, "abc123"},
		{"top aliases", `"Peer":` + peer + `,"SERVER":{"id":"S0","name":"nats-0","future":{"raw":"private server"}},"TYPE":` + string(base["type"]) + `,"ID":"x","TIMESTAMP":` + string(base["timestamp"]) + `,"KIND":"Client","REASON":"client not OCSP valid","future":{"peer":{"subject":"private top"}}`, "abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := DecodeSys(ocspSubject, []byte(envelope+","+tc.fields+"}"), recv)
			if err != nil {
				t.Fatal(err)
			}
			if ev.ServerName != "nats-0" || ev.id != "x" || ev.Type != server.OCSPPeerRejectEventMsgType || ev.Reason != "client not OCSP valid" || !ev.Time.Equal(recv.Add(-time.Second)) {
				t.Fatalf("lost event identity: %+v", ev)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(ev.Raw, &raw); err != nil {
				t.Fatal(err)
			}
			for key := range raw {
				if !slices.Contains([]string{"type", "id", "timestamp", "server", "kind", "reason", "peer"}, key) {
					t.Errorf("non-allowlisted key %q", key)
				}
			}
			var safePeer map[string]string
			if err := json.Unmarshal(raw["peer"], &safePeer); err != nil {
				t.Fatal(err)
			}
			if len(safePeer) != 1 || safePeer["fingerprint"] != tc.fingerprint {
				t.Errorf("peer = %v", safePeer)
			}
			for _, secret := range []string{"private", "Y2Vyd", `"raw"`, "future", "subject", "issuer"} {
				if strings.Contains(string(ev.Raw), secret) {
					t.Errorf("retained %q: %s", secret, ev.Raw)
				}
			}
			if !strings.Contains(string(raw["server"]), `"id":"S0"`) || string(raw["kind"]) != `"Client"` || string(raw["id"]) != `"x"` || string(raw["type"]) != string(base["type"]) || string(raw["timestamp"]) != string(base["timestamp"]) || string(raw["reason"]) != string(base["reason"]) {
				t.Errorf("lost safe raw fields: %s", ev.Raw)
			}
		})
	}
}

func TestOrdinarySystemEventKeepsUnknownRaw(t *testing.T) {
	data := marshal(t, server.ConnectEventMsg{TypedEvent: typed(server.ConnectEventMsgType), Server: server.ServerInfo{Name: "nats-0"}})
	data = []byte(strings.TrimSuffix(string(data), "}") + `,"future":{"peer":{"subject":"ordinary field"}}}`)
	ev, err := DecodeSys("$SYS.ACCOUNT.APP.CONNECT", data, recv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ev.Raw), `"future":{"peer":{"subject":"ordinary field"}}`) {
		t.Fatalf("ordinary unknown raw dropped: %s", ev.Raw)
	}
}

func TestOCSPMalformedTimestamp(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(ocspPayload(t), &m); err != nil {
		t.Fatal(err)
	}
	m["TIMESTAMP"] = "not a time"
	if _, err := DecodeSys(ocspSubject, marshal(t, m), recv); err == nil {
		t.Fatal("accepted malformed timestamp alias")
	}
}
