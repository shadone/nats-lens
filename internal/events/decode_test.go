package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

var recv = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

type recorded struct {
	Subject string          `json:"subject"`
	Account string          `json:"account"`
	Payload json.RawMessage `json:"payload"`
}

func loadFixture(t *testing.T, name string) recorded {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "events", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var r recorded
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func decodeRecorded(t *testing.T, r recorded) Event {
	t.Helper()
	var ev Event
	var err error
	if r.Account == "" {
		ev, err = DecodeSys(r.Subject, r.Payload, recv)
	} else {
		ev, err = DecodeJS(r.Account, r.Subject, r.Payload, recv)
	}
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// want lists the fields a decoded event must have; empty fields are not
// checked except where noted.
type want struct {
	kind, source, typ, server, account, stream, consumer, client, reason string
	// payloadTime is true when the payload carries a timestamp, so time
	// must differ from the receive time.
	payloadTime bool
}

func check(t *testing.T, ev Event, w want) {
	t.Helper()
	eq := func(field, got, want string) {
		if want != "" && got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	eq("kind", ev.Kind, w.kind)
	eq("source", ev.Source, w.source)
	eq("type", ev.Type, w.typ)
	eq("server", ev.ServerName, w.server)
	eq("account", ev.Account, w.account)
	eq("stream", ev.Stream, w.stream)
	eq("consumer", ev.Consumer, w.consumer)
	eq("client", ev.ClientName, w.client)
	eq("reason", ev.Reason, w.reason)
	if ev.Summary == "" {
		t.Error("empty summary")
	}
	if !json.Valid(ev.Raw) {
		t.Errorf("raw is not JSON: %s", ev.Raw)
	}
	if w.payloadTime && ev.Time.Equal(recv) {
		t.Error("time should come from the payload")
	}
	if ev.Time.Location() != time.UTC || !ev.ReceivedAt.Equal(recv) {
		t.Errorf("time %v received %v", ev.Time, ev.ReceivedAt)
	}
}

func TestDecodeRecordedPayloads(t *testing.T) {
	const jobs, worker = "order-jobs", "resize-worker"
	sys, js := SourceSys, SourceJetStream
	cases := map[string]want{
		"client_connect":          {kind: "client.connect", source: sys, typ: server.ConnectEventMsgType, account: "APP", payloadTime: true},
		"client_disconnect":       {kind: "client.disconnect", source: sys, server: "nats-1", account: "APP", client: "victim", reason: "Client Closed", payloadTime: true},
		"auth_error":              {kind: "client.auth_error", source: sys, server: "nats-1", reason: "Authentication Failure", payloadTime: true},
		"account_connections":     {kind: "account.connections", source: sys, typ: server.AccountNumConnsMsgType, payloadTime: true},
		"lameduck":                {kind: "server.lameduck", source: sys, server: "nats-2", payloadTime: true},
		"shutdown":                {kind: "server.shutdown", source: sys, server: "nats-1", payloadTime: true},
		"stream_action_create":    {kind: "stream.created", source: js, account: "APP", stream: jobs, payloadTime: true},
		"stream_action_modify":    {kind: "stream.updated", source: js, stream: jobs},
		"stream_action_delete":    {kind: "stream.deleted", source: js, stream: jobs},
		"consumer_action_create":  {kind: "consumer.created", source: js, stream: jobs, consumer: worker},
		"consumer_action_delete":  {kind: "consumer.deleted", source: js, stream: jobs, consumer: worker},
		"consumer_pause":          {kind: "consumer.paused", source: js, stream: jobs, consumer: worker},
		"stream_leader_elected":   {kind: "stream.leader_elected", source: js, account: "APP", stream: jobs, payloadTime: true},
		"consumer_leader_elected": {kind: "consumer.leader_elected", source: js, stream: jobs, consumer: worker},
		"domain_leader_elected":   {kind: "meta.leader_elected", source: js, server: "nats-1"},
		"max_deliver":             {kind: "consumer.max_deliveries", source: js, stream: jobs, consumer: worker},
		"nak":                     {kind: "consumer.nak", source: js, stream: jobs, consumer: worker},
		"terminated":              {kind: "consumer.terminated", source: js, stream: jobs, consumer: worker, reason: "bad payload"},
		"consumer_ack":            {kind: "consumer.ack_sampled", source: js, stream: jobs, consumer: worker},
		"api_audit_consumer_delete": {kind: KindAPIAudit, source: js, typ: server.JSAPIAuditType, account: "APP",
			stream: jobs, consumer: worker, client: "test-deleter", payloadTime: true},
		"api_audit_stream_create": {kind: KindAPIAudit, source: js, stream: jobs},
	}
	for name, w := range cases {
		t.Run(name, func(t *testing.T) {
			ev := decodeRecorded(t, loadFixture(t, name))
			check(t, ev, w)
			if w.kind == "stream.leader_elected" || w.kind == "consumer.leader_elected" || w.kind == "meta.leader_elected" {
				if ev.ServerName == "" || !strings.Contains(ev.Summary, "current") {
					t.Errorf("leader election should name the leader and replicas: %q %q", ev.ServerName, ev.Summary)
				}
			}
		})
	}
}

// Every recorded fixture decodes without error and gets a known kind.
func TestEveryRecordedFixtureDecodes(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "events", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		ev := decodeRecorded(t, loadFixture(t, name))
		if ev.Kind == "" || ev.Kind == "jetstream.other" {
			t.Errorf("%s: kind %q", name, ev.Kind)
		}
	}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func typed(typ string) server.TypedEvent {
	return server.TypedEvent{Type: typ, ID: "x", Time: recv.Add(-time.Second)}
}

// Advisories that are hard to cause in a test cluster, built from the
// exported nats-server types.
func TestDecodeBuiltPayloads(t *testing.T) {
	replicas := []*server.PeerInfo{{Name: "nats-0", Current: true}, {Name: "nats-2", Offline: true}}
	client := &server.ClientInfo{Name: "ops", Host: "10.0.0.9", User: "admin", Lang: "go", Version: "1.54.0"}
	cases := []struct {
		subject string
		payload any
		want    want
	}{
		{"$JS.EVENT.ADVISORY.STREAM.QUORUM_LOST.jobs", server.JSStreamQuorumLostAdvisory{
			TypedEvent: typed(server.JSStreamQuorumLostAdvisoryType), Stream: "jobs", Replicas: replicas,
		}, want{kind: "stream.quorum_lost", stream: "jobs", account: "APP", payloadTime: true}},
		{"$JS.EVENT.ADVISORY.CONSUMER.QUORUM_LOST.jobs.w", server.JSConsumerQuorumLostAdvisory{
			TypedEvent: typed(server.JSConsumerQuorumLostAdvisoryType), Account: "OTHER", Stream: "jobs", Consumer: "w", Replicas: replicas,
		}, want{kind: "consumer.quorum_lost", stream: "jobs", consumer: "w", account: "OTHER"}},
		{"$JS.EVENT.ADVISORY.SERVER.OUT_OF_STORAGE", server.JSServerOutOfSpaceAdvisory{
			TypedEvent: typed(server.JSServerOutOfStorageAdvisoryType), Server: "nats-1", Stream: "jobs",
		}, want{kind: "server.out_of_storage", server: "nats-1", stream: "jobs"}},
		{"$JS.EVENT.ADVISORY.SERVER.REMOVED", server.JSServerRemovedAdvisory{
			TypedEvent: typed(server.JSServerRemovedAdvisoryType), Server: "nats-2",
		}, want{kind: "server.removed", server: "nats-2"}},
		{"$JS.EVENT.ADVISORY.API.LIMIT_REACHED", server.JSAPILimitReachedAdvisory{
			TypedEvent: typed(server.JSAPILimitReachedAdvisoryType), Server: "nats-0", Dropped: 12,
		}, want{kind: "jetstream.api_limit_reached", server: "nats-0"}},
		{"$JS.EVENT.ADVISORY.META.RESCUE", server.JSMetaRescueAdvisory{
			TypedEvent: typed(server.JSMetaRescueAdvisoryType), Server: "nats-0", PrevQuorum: 2, NewQuorum: 1,
		}, want{kind: "meta.rescue", server: "nats-0"}},
		{"$JS.EVENT.ADVISORY.STREAM.BATCH_ABANDONED.jobs", server.JSStreamBatchAbandonedAdvisory{
			TypedEvent: typed(server.JSStreamBatchAbandonedAdvisoryType), Stream: "jobs", Reason: server.BatchTimeout,
		}, want{kind: "stream.batch_abandoned", stream: "jobs", reason: "timeout"}},
		{"$JS.EVENT.ADVISORY.CONSUMER.PINNED.jobs.w", server.JSConsumerGroupPinnedAdvisory{
			TypedEvent: typed(server.JSConsumerGroupPinnedAdvisoryType), Stream: "jobs", Consumer: "w", Group: "g", PinnedClientId: "p1",
		}, want{kind: "consumer.pinned", stream: "jobs", consumer: "w"}},
		{"$JS.EVENT.ADVISORY.CONSUMER.UNPINNED.jobs.w", server.JSConsumerGroupUnpinnedAdvisory{
			TypedEvent: typed(server.JSConsumerGroupUnpinnedAdvisoryType), Stream: "jobs", Consumer: "w", Group: "g", Reason: "timeout",
		}, want{kind: "consumer.unpinned", reason: "timeout"}},
		{"$JS.EVENT.ADVISORY.STREAM.SNAPSHOT_CREATE.jobs", server.JSSnapshotCreateAdvisory{
			TypedEvent: typed(server.JSSnapshotCreatedAdvisoryType), Stream: "jobs", Client: client,
		}, want{kind: "stream.snapshot_started", stream: "jobs", client: "ops"}},
		{"$JS.EVENT.ADVISORY.STREAM.SNAPSHOT_COMPLETE.jobs", server.JSSnapshotCompleteAdvisory{
			TypedEvent: typed(server.JSSnapshotCompleteAdvisoryType), Stream: "jobs", Client: client, Error: "disk",
		}, want{kind: "stream.snapshot_completed", client: "ops", reason: "disk"}},
		{"$JS.EVENT.ADVISORY.STREAM.RESTORE_CREATE.jobs", server.JSRestoreCreateAdvisory{
			TypedEvent: typed(server.JSRestoreCreateAdvisoryType), Stream: "jobs", Client: client,
		}, want{kind: "stream.restore_started", client: "ops"}},
		{"$JS.EVENT.ADVISORY.STREAM.RESTORE_COMPLETE.jobs", server.JSRestoreCompleteAdvisory{
			TypedEvent: typed(server.JSRestoreCompleteAdvisoryType), Stream: "jobs", Client: client,
		}, want{kind: "stream.restore_completed", client: "ops"}},
		{"$JS.EVENT.ADVISORY.CONSUMER.PAUSE.jobs.w", server.JSConsumerPauseAdvisory{
			TypedEvent: typed(server.JSConsumerPauseAdvisoryType), Stream: "jobs", Consumer: "w", Paused: false,
		}, want{kind: "consumer.resumed", consumer: "w"}},
		{"$JS.EVENT.ADVISORY.FUTURE", map[string]any{"type": "io.nats.jetstream.advisory.v9.future", "stream": "jobs"},
			want{kind: "jetstream.other", typ: "io.nats.jetstream.advisory.v9.future"}},
	}
	for _, c := range cases {
		t.Run(c.subject, func(t *testing.T) {
			ev, err := DecodeJS("APP", c.subject, marshal(t, c.payload), recv)
			if err != nil {
				t.Fatal(err)
			}
			c.want.source = SourceJetStream
			check(t, ev, c.want)
		})
	}
}

func TestDecodeRemovesJWT(t *testing.T) {
	m := server.ConnectEventMsg{
		TypedEvent: typed(server.ConnectEventMsgType),
		Server:     server.ServerInfo{Name: "nats-0"},
		Client:     server.ClientInfo{Name: "svc", Account: "APP", Jwt: "eyJ0eXAiOiJKV1QiLCJhbGciOiJlZDI1NTE5In0.secret"},
	}
	ev, err := DecodeSys("$SYS.ACCOUNT.APP.CONNECT", marshal(t, m), recv)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ev.Raw), "eyJ0eXAi") || strings.Contains(string(ev.Raw), `"jwt"`) {
		t.Fatalf("JWT kept in raw: %s", ev.Raw)
	}
	if !strings.Contains(string(ev.Raw), `"name":"svc"`) {
		t.Fatalf("client fields lost: %s", ev.Raw)
	}
}

func TestCleanRawRemovesJWTAtAnyDepth(t *testing.T) {
	in := `{"client":{"name":"svc","jwt":"eyJa.one"},"jwt":"eyJb.two",` +
		`"nested":{"deeper":{"JWT":"eyJc.three","keep":1}},` +
		`"list":[{"jwt":"eyJd.four","id":12345678901234567890},"x"]}`
	raw, err := cleanRaw([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "eyJ") || strings.Contains(strings.ToLower(s), `"jwt"`) {
		t.Fatalf("JWT kept in raw: %s", s)
	}
	for _, want := range []string{`"name":"svc"`, `"keep":1`, `"id":12345678901234567890`, `"x"`} {
		if !strings.Contains(s, want) {
			t.Errorf("%s lost: %s", want, s)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := DecodeSys("$SYS.ACCOUNT.X.CONNECT", []byte("not json"), recv); err == nil {
		t.Error("expected an error for a non-JSON payload")
	}
	if _, err := DecodeSys("$SYS.SOMETHING.ELSE", []byte("{}"), recv); err == nil {
		t.Error("expected an error for an unknown system subject")
	}
	if _, err := DecodeJS("A", "$JS.EVENT.ADVISORY.X", []byte(`{"type":"`+server.JSStreamActionAdvisoryType+`","stream":5}`), recv); err == nil {
		t.Error("expected an error for a mistyped field")
	}
}

func TestStartEvents(t *testing.T) {
	evs := StartEvents(recv.Add(-90*time.Minute), recv, "dev")
	if len(evs) != 2 || evs[0].Kind != "collector.gap" || evs[1].Kind != "collector.started" {
		t.Fatalf("events = %+v", evs)
	}
	if !evs[0].Time.Equal(recv.Add(-90*time.Minute)) || !strings.Contains(evs[0].Summary, "1h30m0s") {
		t.Errorf("gap = %+v", evs[0])
	}
	if evs := StartEvents(time.Time{}, recv, "dev"); len(evs) != 1 || evs[0].Kind != "collector.started" {
		t.Fatalf("empty database should record no gap: %+v", evs)
	}
}

// A connect event's client name tag is the account's readable name.
func TestDecodeAccountNameTag(t *testing.T) {
	payload := `{"type":"io.nats.server.advisory.v1.client_connect","id":"x","timestamp":"2026-10-17T09:00:00Z",` +
		`"server":{"name":"nats-0","id":"N0"},"client":{"id":5,"acc":"ACJQ","name_tag":"APP","name":"jetstream-controller"}}`
	ev, err := DecodeSys("$SYS.ACCOUNT.ACJQ.CONNECT", []byte(payload), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Account != "ACJQ" || ev.AccountName != "APP" {
		t.Errorf("account %q name %q, want ACJQ APP", ev.Account, ev.AccountName)
	}
}

// Only connect and disconnect events carry the account's name tag; an
// API audit's client name tag is the requesting user's, so it never names
// the account.
func TestAuditNameTagIsUserNotAccount(t *testing.T) {
	payload := `{"type":"io.nats.jetstream.advisory.v1.api_audit","id":"x","timestamp":"2026-10-17T09:00:00Z",` +
		`"server":"nats-0","client":{"id":5,"acc":"ACJQ","name_tag":"alice-the-user","name":"nats cli"},` +
		`"subject":"$JS.API.STREAM.CREATE.X","request":"{}","response":"{}"}`
	ev, err := DecodeJS("ACJQ", "$JS.EVENT.ADVISORY.API", []byte(payload), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ev.AccountName != "" {
		t.Errorf("account name set from a user name tag: %q", ev.AccountName)
	}
	disc := `{"type":"io.nats.server.advisory.v1.client_disconnect","id":"y","timestamp":"2026-10-17T09:00:00Z",` +
		`"server":{"name":"nats-0","id":"N0"},"client":{"id":5,"acc":"ACJQ","name_tag":"APP","name":"w"},"reason":"Client Closed"}`
	if ev, err = DecodeSys("$SYS.ACCOUNT.ACJQ.DISCONNECT", []byte(disc), time.Now()); err != nil || ev.AccountName != "APP" {
		t.Errorf("disconnect account name %q, %v", ev.AccountName, err)
	}
}
