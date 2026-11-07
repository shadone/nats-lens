package collect

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// setAt returns the JSON b with the value at path (object keys and list
// positions) set to value; the last key is added when missing.
func setAt(t *testing.T, b []byte, value any, path ...any) []byte {
	t.Helper()
	var tree any
	if err := json.Unmarshal(b, &tree); err != nil {
		t.Fatal(err)
	}
	node := tree
	for i, p := range path {
		last := i == len(path)-1
		switch k := p.(type) {
		case string:
			obj := node.(map[string]any)
			if last {
				obj[k] = value
			} else {
				node = obj[k]
			}
		case int:
			list := node.([]any)
			if last {
				list[k] = value
			} else {
				node = list[k]
			}
		}
	}
	out, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// In jsz-nats-1.json, account APP's stream 4 is order-jobs, and its
// consumer 0 is resize-worker.
var (
	orderJobs    = []any{"data", "account_details", 0, "stream_detail", 4}
	resizeWorker = append(append([]any{}, orderJobs...), "consumer_detail", 0)
)

func at(base []any, rest ...any) []any { return append(append([]any{}, base...), rest...) }

// unknownCases are one unknown value for each kind of enum field JSZ
// carries: stream configuration, consumer configuration, and the
// configuration nested in a stream's cluster information.
var unknownCases = []struct {
	name, path string
	at         []any
	field      func(*server.StreamDetail) any
}{
	{"retention", "account_details[0].stream_detail[4].config.retention", at(orderJobs, "config", "retention"),
		func(s *server.StreamDetail) any { return s.Config.Retention }},
	{"storage", "account_details[0].stream_detail[4].config.storage", at(orderJobs, "config", "storage"),
		func(s *server.StreamDetail) any { return s.Config.Storage }},
	{"discard", "account_details[0].stream_detail[4].config.discard", at(orderJobs, "config", "discard"),
		func(s *server.StreamDetail) any { return s.Config.Discard }},
	{"compression", "account_details[0].stream_detail[4].config.compression", at(orderJobs, "config", "compression"),
		func(s *server.StreamDetail) any { return s.Config.Compression }},
	{"persist_mode", "account_details[0].stream_detail[4].config.persist_mode", at(orderJobs, "config", "persist_mode"),
		func(s *server.StreamDetail) any { return s.Config.PersistMode }},
	{"deliver_policy", "account_details[0].stream_detail[4].consumer_detail[0].config.deliver_policy", at(resizeWorker, "config", "deliver_policy"),
		func(s *server.StreamDetail) any { return s.Consumer[0].Config.DeliverPolicy }},
	{"ack_policy", "account_details[0].stream_detail[4].consumer_detail[0].config.ack_policy", at(resizeWorker, "config", "ack_policy"),
		func(s *server.StreamDetail) any { return s.Consumer[0].Config.AckPolicy }},
	{"replay_policy", "account_details[0].stream_detail[4].consumer_detail[0].config.replay_policy", at(resizeWorker, "config", "replay_policy"),
		func(s *server.StreamDetail) any { return s.Consumer[0].Config.ReplayPolicy }},
	{"priority_policy", "account_details[0].stream_detail[4].consumer_detail[0].config.priority_policy", at(resizeWorker, "config", "priority_policy"),
		func(s *server.StreamDetail) any { return s.Consumer[0].Config.PriorityPolicy }},
	{"desired origin retention", "account_details[0].stream_detail[4].cluster.desired.origin.retention", at(orderJobs, "cluster", "desired"),
		func(s *server.StreamDetail) any { return s.Cluster.Desired.Origin.Retention }},
}

// notKnown are the spellings nats-server's encoding gives a value it does
// not know, where it gives one rather than an error: a deliver policy
// encodes as "undefined", a health error type as "unknown".
var notKnown = map[string]bool{`"undefined"`: true, `"unknown"`: true}

func caseValue(name string) any {
	if name == "desired origin retention" {
		return map[string]any{"origin": map[string]any{"replicas": 3, "retention": "future"}}
	}
	return "future"
}

// jszCounts is the shape of a JSZ reply: streams and consumers per
// account, and each stream's subjects.
func jszCounts(d *server.JSInfo) string {
	var b strings.Builder
	for _, a := range d.AccountDetails {
		b.WriteString(a.Name + ":")
		for _, s := range a.Streams {
			b.WriteString(" " + s.Name + "/" + strings.Join(s.Config.Subjects, ","))
			for _, c := range s.Consumer {
				b.WriteString(" " + c.Name)
			}
		}
		b.WriteString(";")
	}
	return b.String()
}

func TestUnknownEnumValueKeepsTheJszReply(t *testing.T) {
	plain := readFixture(t, "jsz-nats-1.json")
	base := NewSnapshot(time.Now())
	if err := base.Apply(Jsz, plain); err != nil {
		t.Fatal(err)
	}
	want := jszCounts(base.Servers["NFAKESERVER1ID"].Jsz)
	for _, c := range unknownCases {
		t.Run(c.name, func(t *testing.T) {
			snap := NewSnapshot(time.Now())
			if err := snap.Apply(Jsz, setAt(t, plain, caseValue(c.name), c.at...)); err != nil {
				t.Fatal(err)
			}
			v := snap.Servers["NFAKESERVER1ID"]
			st := v.Status[Jsz]
			if st.State != StateOK || v.Jsz == nil {
				t.Fatalf("status = %+v, want ok with the reply kept", st)
			}
			if got := jszCounts(v.Jsz); got != want {
				t.Errorf("reply = %s, want %s", got, want)
			}
			if len(st.Unknown) != 1 || st.Unknown[0].Path != c.path || st.Unknown[0].Value != `"future"` {
				t.Errorf("unknown = %+v, want %s = \"future\"", st.Unknown, c.path)
			}
			s := &v.Jsz.AccountDetails[0].Streams[4]
			if b, err := json.Marshal(c.field(s)); err == nil && !notKnown[string(b)] {
				t.Errorf("%s holds %s, a known value; want one that does not encode", c.name, b)
			}
		})
	}
}

func TestEveryUnknownEnumValueAtOnce(t *testing.T) {
	b := readFixture(t, "jsz-nats-1.json")
	for _, c := range unknownCases {
		b = setAt(t, b, caseValue(c.name), c.at...)
	}
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Jsz, b); err != nil {
		t.Fatal(err)
	}
	st := snap.Servers["NFAKESERVER1ID"].Status[Jsz]
	if st.State != StateOK || len(st.Unknown) != len(unknownCases) {
		t.Fatalf("status = %+v, want ok with %d unknown values", st, len(unknownCases))
	}
	s := snap.Servers["NFAKESERVER1ID"].Jsz.AccountDetails[0].Streams[4]
	if s.Name != "order-jobs" || s.Config.MaxBytes == 0 || len(s.Consumer) != 2 || s.Consumer[0].Config.Durable != "resize-worker" {
		t.Errorf("the rest of order-jobs is lost: %+v", s.Config)
	}
}

func TestOtherDecodeErrorsStillFail(t *testing.T) {
	b := setAt(t, readFixture(t, "jsz-nats-1.json"), "future", at(orderJobs, "config", "retention")...)
	b = setAt(t, b, "lots", at(orderJobs, "config", "max_msgs")...)
	snap := NewSnapshot(time.Now())
	err := snap.Apply(Jsz, b)
	if err == nil || !strings.Contains(err.Error(), "max_msgs") {
		t.Errorf("err = %v, want the max_msgs error", err)
	}
	if len(snap.Servers) != 0 {
		t.Errorf("servers = %v, want none from a reply that does not decode", snap.Servers)
	}
}

func TestUnknownHealthzErrorType(t *testing.T) {
	b := []byte(`{"server":{"name":"nats-0","id":"N0"},"data":{"status":"unavailable","status_code":503,
		"errors":[{"type":"FUTURE","error":"something new"},{"type":"STREAM","stream":"orders","error":"not current"}]}}`)
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Healthz, b); err != nil {
		t.Fatal(err)
	}
	v := snap.Servers["N0"]
	if v.Status[Healthz].State != StateOK || v.Healthz == nil || len(v.Healthz.Errors) != 2 {
		t.Fatalf("status = %+v, healthz = %+v", v.Status[Healthz], v.Healthz)
	}
	if got := v.Healthz.Errors[0].Type.String(); got != "unknown" {
		t.Errorf("unknown type reads %q, want unknown", got)
	}
	if v.Healthz.Errors[1].Type != server.HealthzErrorStream || v.Healthz.Errors[0].Error != "something new" {
		t.Errorf("errors = %+v", v.Healthz.Errors)
	}
}

func TestUnknownValuesLoggedOncePerServerAndField(t *testing.T) {
	var buf bytes.Buffer
	c := &Collector{log: slog.New(slog.NewTextHandler(&buf, nil))}
	b := setAt(t, readFixture(t, "jsz-nats-1.json"), "future", at(orderJobs, "config", "retention")...)
	b = setAt(t, b, "future", "data", "account_details", 0, "stream_detail", 3, "config", "retention")
	b = setAt(t, b, "future", at(resizeWorker, "config", "ack_policy")...)
	for range 3 {
		snap := NewSnapshot(time.Now())
		if err := snap.Apply(Jsz, b); err != nil {
			t.Fatal(err)
		}
		c.logUnknownValues(snap)
	}
	out := buf.String()
	if n := strings.Count(out, "does not know"); n != 2 {
		t.Fatalf("%d log lines, want 2 (retention, ack policy):\n%s", n, out)
	}
	for _, s := range []string{
		"field=account_details[].stream_detail[].config.retention", "places=2",
		"first=account_details[0].stream_detail[3].config.retention",
		"field=account_details[].stream_detail[].consumer_detail[].config.ack_policy",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("log lacks %s:\n%s", s, out)
		}
	}
}
