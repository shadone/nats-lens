package checks

import (
	"testing"
	"time"
)

func TestNotificationsNotDelivered(t *testing.T) {
	f := newFx(t)
	f.epochAt(1, t0, three)
	f.row("notify_outbox", m{"id": 1, "channel": "ops", "created_at": t0.Add(-11 * time.Minute), "epoch": 1, "kind": "baseline", "dedup_key": "one", "message": "{}"})
	r := f.fires("system-010", 1, "")
	want(t, r[""], "warning", "ops")
	f.exec(`UPDATE notify_outbox SET state='refused', finished_at=?, last_status=400`, t0)
	r = f.fires("system-010", 1, "")
	want(t, r[""], "critical", "400")
	f.exec(`UPDATE notify_outbox SET finished_at=?`, t0.Add(-time.Hour-time.Second))
	f.fires("system-010", 1)
	g := newFx(t)
	g.epochAt(1, t0, three)
	g.fires("system-010", 1)
}
