package events

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func recordedEvent(t *testing.T, name string) (Event, []byte) {
	t.Helper()
	r := loadFixture(t, name)
	return decodeRecorded(t, r), r.Payload
}

func TestCorrelatorAuditAfterAction(t *testing.T) {
	c := newCorrelator()
	del, delRaw := recordedEvent(t, "consumer_action_delete")
	if out := c.add("APP", del, delRaw, recv); len(out) != 0 {
		t.Fatalf("action should wait for its audit, got %d events", len(out))
	}
	audit, auditRaw := recordedEvent(t, "api_audit_consumer_delete")
	out := c.add("APP", audit, auditRaw, recv.Add(100*time.Millisecond))
	if len(out) != 2 || out[1].Kind != "consumer.deleted" {
		t.Fatalf("out = %+v", out)
	}
	got := out[1]
	if got.ClientName != "test-deleter" || got.ClientHost != "127.0.0.1" || got.ClientUser != "app" {
		t.Errorf("client not attached: %+v", got)
	}
	if !strings.Contains(got.Summary, "test-deleter") {
		t.Errorf("summary = %q", got.Summary)
	}
	if len(c.held) != 0 || len(c.audits) != 0 {
		t.Error("correlator kept state")
	}
}

func TestCorrelatorAuditBeforeAction(t *testing.T) {
	c := newCorrelator()
	audit, auditRaw := recordedEvent(t, "api_audit_consumer_delete")
	if out := c.add("APP", audit, auditRaw, recv); len(out) != 1 || out[0].Kind != KindAPIAudit {
		t.Fatalf("audit should pass through: %+v", out)
	}
	del, delRaw := recordedEvent(t, "consumer_action_delete")
	out := c.add("APP", del, delRaw, recv.Add(time.Second))
	if len(out) != 1 || out[0].ClientName != "test-deleter" {
		t.Fatalf("out = %+v", out)
	}
}

func TestCorrelatorActionWithoutAudit(t *testing.T) {
	c := newCorrelator()
	del, delRaw := recordedEvent(t, "consumer_action_delete")
	c.add("APP", del, delRaw, recv)
	if out := c.expire(recv.Add(time.Second)); len(out) != 0 {
		t.Fatal("released before the hold time")
	}
	out := c.expire(recv.Add(holdFor))
	if len(out) != 1 || out[0].ClientName != "" {
		t.Fatalf("out = %+v", out)
	}
}

func TestCorrelatorIgnoresOtherAccountsAndFailures(t *testing.T) {
	c := newCorrelator()
	audit, auditRaw := recordedEvent(t, "api_audit_consumer_delete")
	c.add("OTHER", audit, auditRaw, recv)
	del, delRaw := recordedEvent(t, "consumer_action_delete")
	if out := c.add("APP", del, delRaw, recv); len(out) != 0 {
		t.Fatal("an audit from another account must not match")
	}

	failed := server.JSAPIAudit{
		TypedEvent: typed(server.JSAPIAuditType), Server: "nats-0",
		Client:   &server.ClientInfo{Name: "x"},
		Subject:  "$JS.API.STREAM.DELETE.jobs",
		Response: `{"type":"io.nats.jetstream.api.v1.stream_delete_response","error":{"code":404,"err_code":10059,"description":"stream not found"}}`,
	}
	raw := marshal(t, failed)
	ev, err := DecodeJS("APP", "$JS.EVENT.ADVISORY.API", raw, recv)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Reason != "404 stream not found" {
		t.Errorf("reason = %q", ev.Reason)
	}
	c.add("APP", ev, raw, recv)
	if _, ok := c.audits[action{account: "APP", op: "stream.deleted", stream: "jobs"}]; ok {
		t.Error("a failed call must not be used for correlation")
	}
}

func TestAuditAction(t *testing.T) {
	cases := map[string]action{
		"$JS.API.STREAM.CREATE.jobs":                {op: "stream.created", stream: "jobs"},
		"$JS.API.STREAM.UPDATE.jobs":                {op: "stream.updated", stream: "jobs"},
		"$JS.API.STREAM.DELETE.jobs":                {op: "stream.deleted", stream: "jobs"},
		"$JS.API.CONSUMER.CREATE.jobs.w":            {op: "consumer.upsert", stream: "jobs", consumer: "w"},
		"$JS.API.CONSUMER.CREATE.jobs.w.app.>":      {op: "consumer.upsert", stream: "jobs", consumer: "w"},
		"$JS.API.CONSUMER.DURABLE.CREATE.jobs.w":    {op: "consumer.upsert", stream: "jobs", consumer: "w"},
		"$JS.API.CONSUMER.DELETE.jobs.w":            {op: "consumer.deleted", stream: "jobs", consumer: "w"},
		"$JS.API.CONSUMER.INFO.jobs.w":              {},
		"$JS.API.STREAM.INFO.jobs":                  {},
		"$JS.API.CONSUMER.MSG.NEXT.jobs.w":          {},
		"$JS.API.CONSUMER.LEADER.STEPDOWN.jobs.w":   {},
		"$JS.API.CONSUMER.CREATE.jobs":              {},
		"$JS.API.STREAM.MSG.DELETE.jobs":            {},
		"$JS.API.CONSUMER.DURABLE.CREATE.jobs.w.xx": {},
	}
	for subj, want := range cases {
		got, ok := auditAction(&server.JSAPIAudit{Subject: subj})
		if ok != (want != action{}) || got != want {
			t.Errorf("%s: got %+v %v, want %+v", subj, got, ok, want)
		}
	}
	// An ephemeral create names the consumer only in the response.
	got, ok := auditAction(&server.JSAPIAudit{Subject: "$JS.API.CONSUMER.CREATE.jobs", Response: `{"name":"eph1"}`})
	if !ok || got.consumer != "eph1" {
		t.Errorf("ephemeral create: %+v %v", got, ok)
	}
}

func auditFor(t *testing.T, client, subject string) (Event, []byte) {
	t.Helper()
	a := server.JSAPIAudit{TypedEvent: typed(server.JSAPIAuditType), Server: "n", Client: &server.ClientInfo{Name: client},
		Subject: subject, Response: `{"name":"w"}`}
	raw := marshal(t, a)
	ev, err := DecodeJS("P", "$JS.EVENT.ADVISORY.API", raw, recv)
	if err != nil {
		t.Fatal(err)
	}
	return ev, raw
}

// An audit of a create that changed nothing (no advisory follows) must not
// name the client of a later, real create of the same consumer.
func TestCorrelatorIgnoresStaleAudit(t *testing.T) {
	c := newCorrelator()
	ev, raw := auditFor(t, "clientA", "$JS.API.CONSUMER.CREATE.jobs.w")
	c.add("P", ev, raw, recv)
	created := Event{Kind: "consumer.created", Stream: "jobs", Consumer: "w", Summary: "consumer created"}
	if out := c.add("P", created, nil, recv.Add(5*time.Second)); len(out) != 0 {
		t.Fatalf("action matched a 5s old audit: %+v", out)
	}
	out := c.expire(recv.Add(5*time.Second + holdFor))
	if len(out) != 1 || out[0].ClientName != "" {
		t.Fatalf("out = %+v", out)
	}
}

// An audit older than the previous action on the same key belongs to that
// action's time, not to the next one.
func TestCorrelatorAuditBeforePreviousAction(t *testing.T) {
	c := newCorrelator()
	created := Event{Kind: "consumer.created", Stream: "jobs", Consumer: "w", Summary: "consumer created"}
	ev, raw := auditFor(t, "clientA", "$JS.API.CONSUMER.CREATE.jobs.w")
	c.add("P", ev, raw, recv)
	// A first create matches the audit.
	if out := c.add("P", created, nil, recv.Add(100*time.Millisecond)); len(out) != 1 || out[0].ClientName != "clientA" {
		t.Fatalf("first create = %+v", out)
	}
	// A second audit arrives before the first create's time is passed on:
	// it is older than the previous action and must not be used.
	ev2, raw2 := auditFor(t, "clientB", "$JS.API.CONSUMER.CREATE.jobs.w")
	c.add("P", ev2, raw2, recv.Add(50*time.Millisecond))
	if out := c.add("P", created, nil, recv.Add(time.Second)); len(out) != 0 {
		t.Fatalf("second create matched an audit older than the first create: %+v", out)
	}
}
