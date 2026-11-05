package checks

import (
	"testing"
	"time"
)

// dropped stores a collector.messages_dropped event of n messages at
// when; n 0 is a slow consumer report, which carries no count.
func (f *fx) dropped(id int64, when time.Time, n int) {
	f.t.Helper()
	cols := m{"source": "collector", "summary": "collector connection sys subscription $SYS.>: nats: slow consumer, messages dropped"}
	if n > 0 {
		cols["summary"] = "collector dropped some event messages: the queue was full or writes kept failing"
		f.row("events", merge(m{"id": id, "time": when, "received_at": when, "kind": "collector.messages_dropped"}, cols))
		f.exec(`UPDATE events SET raw = json_object('dropped', ?::BIGINT) WHERE id = ?`, n, id)
		return
	}
	f.event(id, when, "collector.messages_dropped", cols)
}

// Messages dropped within the lookback of an hour fire; none, or those
// dropped more than an hour ago, do not.
func TestEventMessagesDropped(t *testing.T) {
	f := newFx(t)
	f.epochAt(1, t0.Add(2*time.Hour), three)
	f.dropped(1, t0.Add(55*time.Minute), 40)
	f.dropped(2, t0.Add(70*time.Minute), 1200)
	r := f.fires("system-009", 1, "")
	want(t, r[""], "warning", "nats-lens dropped 1200 event messages in the last 1h, the latest at 10:10:00 UTC: collector dropped some event messages")
	if v := r[""].Value; v == nil || *v != 1200 {
		t.Errorf("value %v, want 1200", v)
	}
	if r := f.eval("system-009", 1, map[string]string{"max_dropped": "1200"}); len(r) != 0 {
		t.Fatalf("1200 with max_dropped 1200: %s", rowsText(r))
	}
	f.epochAt(2, t0.Add(3*time.Hour+10*time.Minute), three)
	f.fires("system-009", 2)
	g := newFx(t)
	g.epochAt(1, t0, three)
	g.event(1, t0.Add(-time.Minute), "collector.disconnected", m{"source": "collector"})
	g.fires("system-009", 1)
}

// A slow consumer report counts as one message, and the detail says the
// count is a lower bound.
func TestEventMessagesDroppedSlowConsumer(t *testing.T) {
	f := newFx(t)
	f.epochAt(1, t0.Add(time.Hour), three)
	f.dropped(1, t0.Add(30*time.Minute), 0)
	f.dropped(2, t0.Add(40*time.Minute), 3)
	r := f.fires("system-009", 1, "")
	want(t, r[""], "warning", "dropped at least 4 event messages")
}
