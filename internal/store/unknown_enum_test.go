package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
)

// jszWithUnknownValues is nats-1's JSZ reply with values this nats-server
// version does not know, as a newer server can send: order-jobs (stream
// 4) has an unknown retention, storage, discard, compression and persist
// mode and an unknown desired-state origin retention; its consumer
// resize-worker an unknown ack, replay and priority policy; consumer
// catalog-writer on catalog-changes (stream 3) only an unknown deliver
// policy.
func jszWithUnknownValues(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../collect/testdata/jsz-nats-1.json")
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	streams := r["data"].(map[string]any)["account_details"].([]any)[0].(map[string]any)["stream_detail"].([]any)
	jobs := streams[4].(map[string]any)
	cfg := jobs["config"].(map[string]any)
	for _, k := range []string{"retention", "storage", "discard", "compression", "persist_mode"} {
		cfg[k] = "future"
	}
	jobs["cluster"].(map[string]any)["desired"] = map[string]any{"origin": map[string]any{"replicas": 3, "retention": "future"}}
	ccfg := jobs["consumer_detail"].([]any)[0].(map[string]any)["config"].(map[string]any)
	for _, k := range []string{"ack_policy", "replay_policy", "priority_policy"} {
		ccfg[k] = "future"
	}
	catalog := streams[3].(map[string]any)["consumer_detail"].([]any)[0].(map[string]any)
	catalog["config"].(map[string]any)["deliver_policy"] = "future"
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestUnknownEnumValuesKeepTheRestOfJSZ writes two epochs of a JSZ reply
// with unknown enum values. Every stream's and consumer's stats are
// stored; a configuration holding an unknown value that nats-server's
// encoding refuses is not stored, the other configurations are, and no
// configuration starts a new version in the second epoch. A deliver policy
// the server does not know reads "undefined", nats-server's own spelling.
func TestUnknownEnumValuesKeepTheRestOfJSZ(t *testing.T) {
	s := openMem(t)
	b := jszWithUnknownValues(t)
	var epochs []int64
	for i := range 2 {
		start := t0.Add(time.Duration(i) * 30 * time.Second)
		snap := collect.NewSnapshot(start)
		mustApply(t, snap, collect.Jsz, b)
		if st := snap.Servers["NFAKESERVER1ID"].Status[collect.Jsz]; st.State != collect.StateOK {
			t.Fatalf("JSZ status = %+v", st)
		}
		snap.FinishedAt = start
		e, err := s.WriteSnapshot(context.Background(), snap)
		if err != nil {
			t.Fatal(err)
		}
		epochs = append(epochs, e)
	}
	db := s.DB()
	for _, c := range []struct {
		q    string
		want int
	}{
		{`SELECT count(*) FROM stream_stats`, 10},
		{`SELECT count(*) FROM stream_stats WHERE stream = 'order-jobs'`, 2},
		{`SELECT count(*) FROM consumer_stats WHERE consumer = 'resize-worker'`, 2},
		{`SELECT count(*) FROM consumer_stats`, 12},
		{`SELECT count(*) FROM stream_config WHERE stream = 'order-jobs'`, 0},
		{`SELECT count(*) FROM stream_config`, 4},
		{`SELECT count(*) FROM stream_config WHERE first_epoch = ? AND last_epoch = ?`, 4},
		{`SELECT count(*) FROM consumer_config WHERE consumer = 'resize-worker'`, 0},
		{`SELECT count(*) FROM consumer_config`, 5},
		{`SELECT count(*) FROM consumer_config WHERE first_epoch = ? AND last_epoch = ?`, 5},
		{`SELECT count(*) FROM consumer_config WHERE consumer = 'catalog-writer' AND deliver_policy = 'undefined'
		  AND json_extract_string(config, '$.deliver_policy') = 'undefined'`, 1},
	} {
		var args []any
		if c.q[len(c.q)-1] == '?' {
			args = []any{epochs[0], epochs[1]}
		}
		if got := count(t, db, c.q, args...); got != c.want {
			t.Errorf("%s = %d, want %d", c.q, got, c.want)
		}
	}
}
