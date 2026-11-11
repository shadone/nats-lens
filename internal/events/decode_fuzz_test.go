package events

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// holdsJWT reports whether v, decoded from JSON, has an object key named
// jwt in any letter case at any depth.
func holdsJWT(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if strings.EqualFold(k, "jwt") || holdsJWT(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if holdsJWT(e) {
				return true
			}
		}
	}
	return false
}

// FuzzDecodeEvent decodes system events and JetStream advisories: a
// payload is refused with an error and no event, or decoded the same way
// each time into an event with a kind, its source, the receive time and a
// raw payload that is a JSON object without any user JWT.
func FuzzDecodeEvent(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("testdata", "events", "*.json"))
	if err != nil || len(paths) == 0 {
		f.Fatalf("no recorded events: %v", err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		var r recorded
		if err := json.Unmarshal(b, &r); err != nil {
			f.Fatal(err)
		}
		f.Add(r.Subject, r.Account, []byte(r.Payload))
	}
	for _, s := range []struct{ subject, account, payload string }{
		{"$SYS.ACCOUNT.APP.CONNECT", "", `{"type":"io.nats.server.advisory.v1.client_connect","client":{"jwt":"eyJ","name":"a","nested":{"JWT":"x","list":[{"Jwt":"y"}]}}}`},
		{"$SYS.ACCOUNT.APP.DISCONNECT", "", `{"type":"io.nats.server.advisory.v1.client_disconnect","client":{"user_jwt":"keep","jwt":null}}`},
		{"$SYS.ACCOUNT.APP.DISCONNECT", "", `{"client":{"start":"not a time"}}`},
		{"$SYS.SERVER.S1.SHUTDOWN", "", `{"server":{"name":"s","id":"S1"}}`},
		{"$SYS.SERVER.S1.LAMEDUCK", "", `{}`},
		{"$SYS.ACCOUNT..CONNECT", "", `{}`},
		{"$SYS.ACCOUNT.APP.CONNECT.EXTRA", "", `{}`},
		{"$SYS.ACCOUNT.APP.CONNECT", "", `[]`},
		{"$SYS.ACCOUNT.APP.CONNECT", "", `{"timestamp":"0001-01-01T00:00:00Z"}`},
		{"$SYS.ACCOUNT.APP.CONNECT", "", `{"timestamp":"9999-12-31T23:59:59.999999999+14:00"}`},
		{"$SYS.ACCOUNT.APP.CONNECT", "", `{"a":1e400}`},
		{"$SYS.ACCOUNT.APP.CONNECT", "", `{"a":12345678901234567890123,"b":"\ud800"} trailing`},
		{"$JS.EVENT.ADVISORY.STREAM.CREATED.orders", "APP", `{"type":"io.nats.jetstream.advisory.v1.stream_action","stream":"orders","action":"future"}`},
		{"$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.orders.w", "APP", `{"type":"io.nats.jetstream.advisory.v1.max_deliver","stream":"orders","consumer":"w","deliveries":"many"}`},
		{"$JS.EVENT.ADVISORY.API", "APP", `{"type":"io.nats.jetstream.advisory.v1.api_audit","client":{"jwt":"secret"},"request":"{\"jwt\":\"inner\"}","response":"{"}`},
		{"$JS.EVENT.ADVISORY.X", "APP", `{"type":"io.nats.jetstream.advisory.v99.unknown","id":"x"}`},
		{"$JS.EVENT.ADVISORY.X", "", `{"type":7}`},
	} {
		f.Add(s.subject, s.account, []byte(s.payload))
	}
	received := time.Date(2026, 10, 17, 14, 30, 0, 0, time.FixedZone("x", 3600))
	f.Fuzz(func(t *testing.T, subject, account string, data []byte) {
		if len(data) > 64<<10 || len(subject) > 1024 || len(account) > 256 {
			return
		}
		decode := func() (Event, error) {
			if account == "" {
				return DecodeSys(subject, data, received)
			}
			return DecodeJS(account, subject, data, received)
		}
		ev, err := decode()
		ev2, err2 := decode()
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(ev, ev2) {
			t.Fatalf("decoding %q on %s is not deterministic: %v / %v", data, subject, err, err2)
		}
		if err != nil {
			if !reflect.DeepEqual(ev, Event{}) {
				t.Fatalf("refused payload returned %+v", ev)
			}
			return
		}
		if ev.Kind == "" || (ev.Source != SourceSys && ev.Source != SourceJetStream) || !ev.ReceivedAt.Equal(received) || ev.ReceivedAt.Location() != time.UTC {
			t.Fatalf("decoded event has kind %q, source %q, received %v", ev.Kind, ev.Source, ev.ReceivedAt)
		}
		dec := json.NewDecoder(bytes.NewReader(ev.Raw))
		dec.UseNumber()
		var raw any
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("raw payload %q is not JSON: %v", ev.Raw, err)
		}
		if _, ok := raw.(map[string]any); !ok {
			t.Fatalf("raw payload %q is not an object", ev.Raw)
		}
		if holdsJWT(raw) {
			t.Fatalf("raw payload keeps a JWT: %s", ev.Raw)
		}
	})
}
