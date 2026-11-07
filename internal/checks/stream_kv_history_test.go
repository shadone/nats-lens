package checks

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// streamEvalWith runs the given checks together, suppression included, and
// returns each one's rows by entity key.
func (f *fx) streamEvalWith(epoch int64, codes ...string) map[string]map[string]store.FindingRow {
	f.t.Helper()
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		f.t.Fatal(err)
	}
	only := map[string]bool{}
	for _, c := range codes {
		only[c] = true
	}
	runs, err := (&Engine{Set: set, only: only}).Evaluate(context.Background(), f.st.DB(), epoch)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]map[string]store.FindingRow{}
	for _, r := range runs {
		if r.Status != store.RunOK {
			f.t.Fatalf("%s: %s %s", r.Code, r.Status, r.Error)
		}
		out[r.Code] = map[string]store.FindingRow{}
		for _, row := range r.Rows {
			out[r.Code][row.EntityKey] = row
		}
	}
	return out
}

const kvMiB = int64(1 << 20)

// streamBucket stores the leader's report of a KV bucket in epoch e.
func (f *fx) streamBucket(e int64, stream string, keys, msgs, bytes int64) {
	f.streamStats(e, "S0", stream, m{"leader": "nats-0", "is_leader": true, "num_subjects": keys, "messages": msgs, "bytes": bytes})
}

func TestKVHistoryWithoutByteLimit(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	ttl := m{"max_age_ns": int64(48 * time.Hour)}
	for _, b := range []struct {
		stream            string
		history, maxBytes int64
		keys, msgs, bytes int64
		noTTL             bool
	}{
		{"KV_prices", 10, -1, 1000, 8000, 200 * kvMiB, false},
		{"KV_edge", 10, -1, 1000, 5000, 100 * kvMiB, false},
		{"KV_small", 10, -1, 1000, 8000, 10 * kvMiB, false},
		{"KV_few", 10, -1, 1000, 3000, 200 * kvMiB, false},
		{"KV_capped", 10, 1 << 30, 1000, 8000, 200 * kvMiB, false},
		{"KV_one", 1, -1, 1000, 1000, 200 * kvMiB, false},
		{"KV_declared", 10, -1, 1000, 8000, 200 * kvMiB, false},
		{"KV_unbounded", 64, -1, 1000, 30000, 300 * kvMiB, true},
		// Not a bucket: a stream's subjects are not keys.
		{"order-events", 10, -1, 1000, 8000, 200 * kvMiB, false},
	} {
		cols := merge(ttl, m{"max_msgs_per_subject": b.history, "max_bytes": b.maxBytes})
		if b.noTTL {
			cols["max_age_ns"] = 0
		}
		f.streamConfig("S0", b.stream, 1, 2, cols)
		f.streamBucket(2, b.stream, b.keys, b.msgs, b.bytes)
	}
	f.row("inventory", m{"account": "APP", "kind": "kv", "name": "declared", "stream": "KV_declared",
		"max_bytes": -1, "source": "inventory.yaml", "loaded_at": t0})
	r := f.fires("stream-019", 2, "APP/KV_prices", "APP/KV_edge", "APP/KV_unbounded")
	want(t, r["APP/KV_prices"], "info",
		"KV bucket prices keeps up to 10 revisions per key without a byte limit: 1000 keys hold 8000 messages (8.0 per key), 200.0 MiB, TTL 2d.")
	if v := r["APP/KV_prices"].Value; v == nil || *v != 8 {
		t.Errorf("value = %v", v)
	}
	// A bucket without a TTL is stream-005's, which names its history.
	both := f.streamEvalWith(2, "stream-005", "stream-019")
	if _, ok := both["stream-005"]["APP/KV_unbounded"]; !ok {
		t.Fatalf("stream-005 = %s", rowsText(both["stream-005"]))
	}
	if len(both["stream-019"]) != 2 {
		t.Fatalf("stream-019 with stream-005 = %s", rowsText(both["stream-019"]))
	}
}

// A firing finding stays down to resolve_ratio (0.8) of the thresholds
// and through a collection without a leader report.
func TestKVHistoryResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	for _, s := range []string{"KV_keeps", "KV_drops", "KV_away", "KV_new"} {
		f.streamConfig("S0", s, 1, 3, m{"max_msgs_per_subject": 10, "max_age_ns": int64(time.Hour)})
	}
	f.streamBucket(3, "KV_keeps", 1000, 4500, 90*kvMiB)
	f.streamBucket(3, "KV_drops", 1000, 3900, 200*kvMiB)
	f.streamBucket(1, "KV_away", 1000, 9000, 200*kvMiB)
	f.streamBucket(3, "KV_new", 1000, 4500, 200*kvMiB)
	for _, s := range []string{"KV_keeps", "KV_drops", "KV_away"} {
		f.streamFiringAt("stream-019", "APP/"+s, 2)
	}
	r := f.fires("stream-019", 3, "APP/KV_keeps", "APP/KV_away")
	want(t, r["APP/KV_away"], "info", "(no leader report in this collection)")
}
