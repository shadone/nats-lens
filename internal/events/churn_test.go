package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func churnEv(kind, name, srv string, cid uint64, reason string, at time.Time) Event {
	return Event{Time: at, ReceivedAt: at, Source: SourceSys, Kind: kind, ServerName: srv, Account: "ACJQ",
		ClientName: name, Reason: reason, Summary: kind, clientID: cid}
}

func churnKinds(evs []Event) string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Kind+":"+e.ClientName)
	}
	return strings.Join(out, ",")
}

func TestChurnCollapsesClosedPairs(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c := newChurn(ChurnOptions{Collapse: true, PairWithin: 10 * time.Second, Window: time.Minute})
	var stored []Event
	add := func(e Event) { stored = append(stored, c.add(e)...) }
	// 28 short-lived connections of one name, each closed by the client.
	for i := range 28 {
		at := t0.Add(time.Duration(i) * 2 * time.Second)
		add(churnEv("client.connect", "jetstream-controller", "nats-0", uint64(100+i), "", at))
		add(churnEv("client.disconnect", "jetstream-controller", "nats-0", uint64(100+i), "Client Closed", at.Add(time.Second)))
	}
	// A disconnect for another reason is stored at once, with its connect.
	add(churnEv("client.connect", "media-worker", "nats-1", 7, "", t0.Add(3*time.Second)))
	add(churnEv("client.disconnect", "media-worker", "nats-1", 7, "Stale Connection", t0.Add(4*time.Second)))
	if got := churnKinds(stored); got != "client.connect:media-worker,client.disconnect:media-worker" {
		t.Fatalf("stored before the window closed: %s", got)
	}
	// A connect that is not closed within pair_within is stored then.
	add(churnEv("client.connect", "ledger-service", "nats-0", 9, "", t0.Add(50*time.Second)))
	stored = append(stored, c.expire(t0.Add(70*time.Second))...)
	got := churnKinds(stored[2:])
	if got != "client.connect:ledger-service,client.churn:jetstream-controller" {
		t.Fatalf("expired = %s", got)
	}
	sum := stored[3]
	if !strings.Contains(sum.Summary, "jetstream-controller connected and disconnected 28 times in 1 min") ||
		sum.ServerName != "nats-0" || sum.Account != "ACJQ" || !sum.Time.Equal(t0) {
		t.Errorf("summary = %+v", sum)
	}
	var raw struct {
		Connects int `json:"connects"`
	}
	if err := json.Unmarshal(sum.Raw, &raw); err != nil || raw.Connects != 28 {
		t.Errorf("raw = %s", sum.Raw)
	}
	if rest := c.flush(); len(rest) != 0 {
		t.Errorf("left after expiry: %s", churnKinds(rest))
	}
}

func TestChurnDisabledPassesEverything(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c := newChurn(ChurnOptions{Collapse: false})
	out := c.add(churnEv("client.connect", "x", "nats-0", 1, "", t0))
	out = append(out, c.add(churnEv("client.disconnect", "x", "nats-0", 1, "Client Closed", t0))...)
	if len(out) != 2 {
		t.Fatalf("disabled: %s", churnKinds(out))
	}
}

func TestChurnFlushStoresHeldConnects(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c := newChurn(ChurnOptions{Collapse: true, PairWithin: 10 * time.Second, Window: time.Minute})
	c.add(churnEv("client.connect", "a", "nats-0", 1, "", t0))
	c.add(churnEv("client.connect", "b", "nats-0", 2, "", t0))
	c.add(churnEv("client.disconnect", "b", "nats-0", 2, "Client Closed", t0.Add(time.Second)))
	if got := churnKinds(c.flush()); got != "client.connect:a,client.churn:b" {
		t.Fatalf("flush = %s", got)
	}
}

// Capture runs the collapsing: a client that connects and closes ten
// times leaves one summary event and no connect or disconnect rows.
func TestCaptureCollapsesChurn(t *testing.T) {
	w := &flakyWriter{}
	c := New(Options{Writer: w, FlushInterval: 20 * time.Millisecond, Log: quiet(),
		Churn: ChurnOptions{Collapse: true, PairWithin: 10 * time.Second, Window: time.Minute}})
	stop := runCapture(t, c)
	msg := func(kind string, cid int, reason string) []byte {
		b, _ := json.Marshal(map[string]any{"type": "io.nats.server.advisory.v1.client_" + kind, "id": kind + string(rune('a'+cid)),
			"timestamp": recv, "server": map[string]any{"name": "nats-0", "id": "N0"},
			"client": map[string]any{"id": cid, "acc": "ACJQ", "name": "jetstream-controller"}, "reason": reason})
		return b
	}
	for i := 1; i <= 10; i++ {
		c.enqueue(incoming{sys: true, subject: "$SYS.ACCOUNT.ACJQ.CONNECT", data: msg("connect", i, ""), at: recv})
		c.enqueue(incoming{sys: true, subject: "$SYS.ACCOUNT.ACJQ.DISCONNECT", data: msg("disconnect", i, "Client Closed"), at: recv})
	}
	time.Sleep(100 * time.Millisecond)
	stop()
	w.mu.Lock()
	defer w.mu.Unlock()
	if got := churnKinds(w.evs); got != "client.churn:jetstream-controller" {
		t.Fatalf("stored %s, want one client.churn", got)
	}
}

// A summary is per client host as well, and carries it, so that a CLI
// from a loopback address stays recognisable as tooling.
func TestChurnSummaryPerHost(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c := newChurn(ChurnOptions{Collapse: true, PairWithin: 10 * time.Second, Window: time.Minute})
	for i, host := range []string{"127.0.0.1", "10.0.0.5", "127.0.0.1"} {
		conn := churnEv("client.connect", "nats", "nats-0", uint64(i+1), "", t0.Add(time.Duration(i)*time.Second))
		conn.ClientHost = host
		disc := churnEv("client.disconnect", "nats", "nats-0", uint64(i+1), "Client Closed", t0.Add(time.Duration(i)*time.Second))
		disc.ClientHost = host
		c.add(conn)
		c.add(disc)
	}
	out := c.flush()
	if len(out) != 2 {
		t.Fatalf("summaries %s, want one per host", churnKinds(out))
	}
	hosts := map[string]bool{}
	for _, e := range out {
		hosts[e.ClientHost] = true
	}
	if !hosts["127.0.0.1"] || !hosts["10.0.0.5"] {
		t.Errorf("hosts %v", hosts)
	}
}
