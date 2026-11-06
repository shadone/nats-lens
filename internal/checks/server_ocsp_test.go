package checks

import (
	"testing"
	"time"
)

func (f *fx) ocsp(id int64, when time.Time, name, eventID string, cols m) {
	f.event(id, when, "server.ocsp_peer_rejected", merge(m{"source": "sys", "server_name": name,
		"type": "io.nats.server.advisory.v1.ocsp_peer_reject", "raw": `{"id":"` + eventID + `","kind":"Client","server":{"id":"S0"}}`}, cols))
}

func TestOCSPPeerRejected(t *testing.T) {
	f := newFx(t)
	f.cluster(70)
	f.ocsp(1, at(39), "nats-0", "one", nil)
	// Duplicate delivered after a collector restart is still one rejection.
	f.ocsp(2, at(40), "nats-0", "one", nil)
	f.ocsp(3, at(40), "event-only", "two", nil)
	f.event(4, at(40), "client.disconnect", m{"server_name": "nats-1", "reason": "OCSP failure"})
	f.event(5, at(40), "server.ocsp_peer_link_invalid", m{"server_name": "nats-2"})
	r := f.fires("server-024", 40, "nats-0", "event-only")
	want(t, r["nats-0"], "warning", "1 OCSP peer connection rejection")
	if r["nats-0"].Value == nil || *r["nats-0"].Value != 1 || r["nats-0"].CauseEventID != 2 {
		t.Fatalf("count/cause: %+v", r["nats-0"])
	}
	f.fires("server-024", 70)
}

func TestOCSPWindowAndIdentity(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.ocsp(1, at(40).Add(-15*time.Minute), "nats-0", "start", nil)
	f.ocsp(2, at(40).Add(time.Second), "nats-0", "future", nil)
	f.ocsp(3, at(40), "nats-1", "delayed", m{"time": at(1)})
	f.ocsp(4, at(40), "nats-2", "wrong-source", m{"source": "collector"})
	f.fires("server-024", 40)
	// A former process's captured name is never reassigned to its renamed server.
	f.exec(`UPDATE servers SET server_name = 'renamed' WHERE server_id = 'S0'`)
	f.ocsp(5, at(40), "nats-0", "old-process", nil)
	f.ocsp(6, at(40), "nats-0", "new-process", m{"raw": `{"id":"new-process","server":{"id":"NEW"}}`})
	f.fires("server-024", 40, "nats-0")
}
