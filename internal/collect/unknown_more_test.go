package collect

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// A null or a number where an enum name belongs is refused by the enum
// like an unknown name, and is left out the same way.
func TestNullAndNumberEnumValuesKeepTheReply(t *testing.T) {
	for _, val := range []any{nil, 2} {
		b := setAt(t, readFixture(t, "jsz-nats-1.json"), val, at(orderJobs, "config", "retention")...)
		snap := NewSnapshot(time.Now())
		if err := snap.Apply(Jsz, b); err != nil {
			t.Fatalf("retention %v loses the reply: %v", val, err)
		}
		st := snap.Servers["NFAKESERVER1ID"].Status[Jsz]
		if len(st.Unknown) != 1 || st.Unknown[0].Path != "account_details[0].stream_detail[4].config.retention" {
			t.Errorf("retention %v: unknown = %+v", val, st.Unknown)
		}
	}
}

// A field of another kind with its own UnmarshalJSON, such as a JWT's
// signing keys (a map), is not taken out: a value it refuses still loses
// the reply rather than reading as no signing keys.
func TestNonEnumUnmarshalerIsNotTakenOut(t *testing.T) {
	b := []byte(`{"server":{"name":"n","id":"N"},"data":{"server_id":"N","now":"2026-01-01T00:00:00Z","account_detail":{"account_name":"A",
	"decoded_jwt":{"name":"shop","nats":{"signing_keys":["AKEY1",{"kind":"future_scope","key":"AKEY2"}]}}}}}`)
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Accountz, b); err == nil {
		t.Errorf("reply decoded with signing keys %v; want the decode error", snap.Servers["N"].Accountz.Account.Claim.SigningKeys)
	}
}

// Unknown values are listed in the order of the reply, stream 2 before
// stream 10.
func TestUnknownValuesInReplyOrder(t *testing.T) {
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Jsz, manyStreams(12, 12)); err != nil {
		t.Fatal(err)
	}
	u := snap.Servers["N"].Status[Jsz].Unknown
	if len(u) != 12 {
		t.Fatalf("unknown = %+v", u)
	}
	for i, x := range u {
		if want := fmt.Sprintf("account_details[0].stream_detail[%d].config.retention", i); x.Path != want {
			t.Errorf("unknown %d = %s, want %s", i, x.Path, want)
		}
	}
}

// An unknown value inside a map of structs and behind pointers.
func TestUnknownValueInMapAndPointer(t *testing.T) {
	type inner struct {
		R *server.RetentionPolicy `json:"r"`
		N int                     `json:"n"`
	}
	var v map[string]map[string]*inner
	u, err := decodeTolerant([]byte(`{"a":{"x":{"r":"future","n":3},"y":{"r":"limits","n":4},"z":{"r":null,"n":5}}}`), &v)
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 1 || v["a"]["x"].N != 3 || v["a"]["x"].R == nil || *v["a"]["x"].R != -1 ||
		*v["a"]["y"].R != server.LimitsPolicy || v["a"]["z"].R != nil {
		t.Errorf("x = %+v, y = %+v, z = %+v, unknown = %+v", v["a"]["x"], v["a"]["y"], v["a"]["z"], u)
	}
}

// manyStreams is a JSZ reply of n streams, the first unknown of them with
// a retention of "future".
func manyStreams(n, unknown int) []byte {
	var sb strings.Builder
	sb.WriteString(`{"server":{"name":"n","id":"N"},"data":{"account_details":[{"name":"A","stream_detail":[`)
	for i := range n {
		if i > 0 {
			sb.WriteByte(',')
		}
		ret := "limits"
		if i < unknown {
			ret = "future"
		}
		fmt.Fprintf(&sb, `{"name":"s%d","created":"2026-01-01T00:00:00Z","config":{"name":"s%d","subjects":["s%d.>"],"retention":%q,"storage":"file","discard":"old","max_msgs":-1,"max_bytes":-1,"max_age":0,"num_replicas":3},"state":{"messages":10,"bytes":100,"first_seq":1,"last_seq":10},"cluster":{"leader":"n","replicas":[{"name":"m","current":true},{"name":"o","current":true}]},"consumer_detail":[{"stream_name":"s%d","name":"c","config":{"durable_name":"c","deliver_policy":"all","ack_policy":"explicit","replay_policy":"instant"},"delivered":{"consumer_seq":1,"stream_seq":1},"num_pending":0}]}`, i, i, i, ret, i)
	}
	sb.WriteString(`]}]}}`)
	return []byte(sb.String())
}

func benchJsz(b *testing.B, unknown int) {
	data := manyStreams(10000, unknown)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		snap := NewSnapshot(time.Now())
		if err := snap.Apply(Jsz, data); err != nil {
			b.Fatal(err)
		}
		if len(snap.Servers["N"].Status[Jsz].Unknown) != unknown {
			b.Fatalf("want %d unknown", unknown)
		}
	}
}

// 10,000 streams: no unknown value, one, and one in every stream.
func BenchmarkJszPlain10k(b *testing.B)    { benchJsz(b, 0) }
func BenchmarkJszFallbackOne(b *testing.B) { benchJsz(b, 1) }
func BenchmarkJszFallback10k(b *testing.B) { benchJsz(b, 10000) }
