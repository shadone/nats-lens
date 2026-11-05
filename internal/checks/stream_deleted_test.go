package checks

import (
	"testing"
	"time"
)

func TestManyDeletedMessages(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	del := func(e int64, stream string, deleted, msgs int64) {
		f.streamLeaderAt(e, stream, m{"num_deleted": deleted, "messages": msgs, "first_seq": 1001,
			"last_seq": 1000 + deleted + msgs, "first_ts": at(e).Add(-3 * 24 * time.Hour)})
	}
	del(3, "media-windows", 4000000, 1000)
	del(3, "KV_stock-levels", 1000000, 1000000)
	// Many deletes, but a small share of a large stream.
	del(3, "order-events", 2000000, 8000000)
	del(3, "fine", 999999, 1)
	// A follower's count is not read.
	f.streamStats(3, "S1", "fine", m{"leader": "nats-0", "is_leader": false, "num_deleted": 9000000, "messages": 1})
	r := f.fires("stream-021", 3, "APP/media-windows", "APP/KV_stock-levels")
	want(t, r["APP/media-windows"], "info",
		"Stream media-windows holds 4000000 deleted messages between sequence 1001 and 4002000 (100.0% of the range, 1000 messages kept); the oldest message is 3 days old.")
	want(t, r["APP/KV_stock-levels"], "info", "KV bucket stock-levels holds 1000000 deleted messages", "(50.0% of the range")
	if r := f.eval("stream-021", 3, map[string]string{"min_deleted_ratio": "0.2"}); len(r) != 3 {
		t.Fatalf("min_deleted_ratio 0.2: %s", rowsText(r))
	}

	// A firing finding stays down to resolve_ratio (0.8) of both
	// thresholds and through a collection without a leader report.
	g := newFx(t)
	g.cluster(3)
	gdel := func(e int64, stream string, deleted, msgs int64) {
		g.streamLeaderAt(e, stream, m{"num_deleted": deleted, "messages": msgs, "first_seq": 1, "last_seq": deleted + msgs})
	}
	gdel(3, "keeps", 850000, 450000)
	gdel(3, "drops", 790000, 10)
	gdel(3, "share", 2000000, 3100000)
	gdel(1, "away", 2000000, 10)
	gdel(3, "new", 900000, 10)
	for _, s := range []string{"keeps", "drops", "share", "away"} {
		g.streamFiringAt("stream-021", "APP/"+s, 2)
	}
	r = g.fires("stream-021", 3, "APP/keeps", "APP/away")
	want(t, r["APP/away"], "info", "(no leader report in this collection)")
}
