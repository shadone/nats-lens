package collect

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// userToken builds a JWT-shaped token with claims; the signature is not
// checked.
func userToken(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"typ":"JWT","alg":"ed25519-nkey"}`)) + "." + enc([]byte(claims)) + "." + enc([]byte("sig"))
}

// The expiry of each user JWT is read, the JWT is removed from the CONNZ
// reply, and a connection without one has no entry.
func TestTakeUserJWTs(t *testing.T) {
	c := &server.Connz{Conns: []*server.ConnInfo{
		{Cid: 1, Account: keyNamed, JWT: userToken(`{"exp":1793000000,"name":"pos-terminal","nats":{"type":"user"}}`)},
		{Cid: 2, Account: keyNamed, JWT: userToken(`{"name":"label-printer"}`)},
		{Cid: 3, Account: keyNamed, JWT: "not.a-jwt.%%%"},
		{Cid: 4, Account: keyNamed, JWT: "garbage"},
		{Cid: 5, Account: "APP"},
		nil,
	}}
	got := takeUserJWTs(c)
	want := map[uint64]UserJWT{
		1: {Decoded: true, Expires: time.Unix(1793000000, 0).UTC()},
		2: {Decoded: true},
		3: {},
		4: {},
	}
	if len(got) != len(want) {
		t.Fatalf("user JWTs %+v, want %+v", got, want)
	}
	for cid, w := range want {
		if g, ok := got[cid]; !ok || g != w {
			t.Errorf("cid %d: %+v, want %+v", cid, g, w)
		}
	}
	for _, ci := range c.Conns {
		if ci != nil && ci.JWT != "" {
			t.Errorf("cid %d keeps its JWT", ci.Cid)
		}
	}
}

// Applying a CONNZ reply keeps the expiry and drops the JWT.
func TestApplyConnzDropsJWT(t *testing.T) {
	snap := NewSnapshot(lookupAt)
	b := []byte(`{"server":{"id":"NSERVER0","name":"nats-0"},"data":{"server_id":"NSERVER0","num_connections":1,
 "connections":[{"cid":7,"account":"` + keyNamed + `","jwt":"` + userToken(`{"exp":1793000000}`) + `"}]}}`)
	if err := snap.Apply(Connz, b); err != nil {
		t.Fatal(err)
	}
	v := snap.Servers["NSERVER0"]
	if v.Connz.Conns[0].JWT != "" {
		t.Error("the snapshot keeps the JWT")
	}
	if j := v.UserJWTs[7]; !j.Decoded || !j.Expires.Equal(time.Unix(1793000000, 0)) {
		t.Errorf("user JWT of cid 7 = %+v", j)
	}
}

// A user JWT that does not decode is logged at debug level with the
// connection, never with the token.
func TestUndecodedJWTLoggedWithoutContent(t *testing.T) {
	const token = "eyJub3QiOiJqc29u.bm90LWpzb24.c2ln"
	snap := NewSnapshot(lookupAt)
	b := []byte(`{"server":{"id":"NSERVER0","name":"nats-0"},"data":{"server_id":"NSERVER0",
 "connections":[{"cid":9,"account":"APP","jwt":"` + token + `"}]}}`)
	if err := snap.Apply(Connz, b); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	c := &Collector{log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	c.logUndecodedJWTs(snap)
	out := buf.String()
	if !strings.Contains(out, "user JWT did not decode") || !strings.Contains(out, "cid=9") {
		t.Errorf("log %q, want the connection named", out)
	}
	for _, part := range strings.Split(token, ".") {
		if strings.Contains(out, part) {
			t.Errorf("log %q holds part of the token", out)
		}
	}
	if j, ok := snap.Servers["NSERVER0"].UserJWTs[9]; !ok || j.Decoded {
		t.Errorf("user JWT of cid 9 = %+v, %v; want present and not decoded", j, ok)
	}
}
