package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

// TestUnknownValueNotesPerServerAndField gives every consumer of nats-1's
// JSZ reply an ack policy this version does not know. Each epoch gets one
// note for the field and value, not one per consumer, and no warning per
// consumer is logged; a configuration that does not encode for another
// reason keeps its own note.
func TestUnknownValueNotesPerServerAndField(t *testing.T) {
	b, err := os.ReadFile("../collect/testdata/jsz-nats-1.json")
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range r["data"].(map[string]any)["account_details"].([]any) {
		ss, _ := a.(map[string]any)["stream_detail"].([]any)
		for _, s := range ss {
			cs, _ := s.(map[string]any)["consumer_detail"].([]any)
			for _, c := range cs {
				c.(map[string]any)["config"].(map[string]any)["ack_policy"] = "future"
				n++
			}
		}
	}
	b, err = json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s := openMem(t)
	var logs bytes.Buffer
	s.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	for i := range 2 {
		start := t0.Add(time.Duration(i) * 30 * time.Second)
		snap := collect.NewSnapshot(start)
		mustApply(t, snap, collect.Jsz, b)
		snap.FinishedAt = start
		if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
	want := fmt.Sprintf(`nats-1 sent consumer config.ack_policy "future", which this nats-server version does not know, for %d consumers; their configurations are not stored`, n)
	if got := count(t, s.DB(), `SELECT sum(len(errors)) FROM epochs`); got != 2 {
		t.Errorf("%d notes over 2 epochs, want 2", got)
	}
	if got := count(t, s.DB(), `SELECT count(*) FROM epochs WHERE errors = [?]`, want); got != 2 {
		var notes string
		_ = s.DB().QueryRow(`SELECT string_agg(array_to_string(errors, ' | '), ' || ') FROM epochs`).Scan(&notes)
		t.Errorf("epochs with the note %q: %d, want 2; notes: %s", want, got, notes)
	}
	if strings.Contains(logs.String(), "does not encode") {
		t.Errorf("a warning per consumer was logged:\n%s", logs.String())
	}
}

// TestUnknownValueNotesNameWhatIsStored reads the notes of the reply with
// unknown values in every field kind: one per field and value, the
// deliver policy's saying it is stored as "undefined".
func TestUnknownValueNotesNameWhatIsStored(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Jsz, jszWithUnknownValues(t))
	snap.FinishedAt = t0
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	var notes []string
	rows, err := s.DB().Query(`SELECT unnest(errors) FROM epochs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		notes = append(notes, n)
	}
	all := strings.Join(notes, "\n")
	for _, w := range []string{
		`nats-1 sent stream config.retention "future", which this nats-server version does not know, for 1 stream; its configuration is not stored`,
		`nats-1 sent consumer config.ack_policy "future", which this nats-server version does not know, for 1 consumer; its configuration is not stored`,
		`nats-1 sent consumer config.deliver_policy "future", which this nats-server version does not know, for 1 consumer; stored as "undefined"`,
	} {
		if !strings.Contains(all, w) {
			t.Errorf("notes lack %q:\n%s", w, all)
		}
	}
	if len(notes) != 9 || strings.Contains(all, "does not encode") {
		t.Errorf("%d notes, want 9 (5 stream fields, 4 consumer fields), none per asset:\n%s", len(notes), all)
	}
}

// TestUnknownNoteFollowsTheConfiguration gives consumer catalog-writer an
// unknown deliver policy and an unknown ack policy. Its configuration is
// not stored, as the ack policy does not encode, so the deliver policy's
// note says so too rather than "stored as undefined".
func TestUnknownNoteFollowsTheConfiguration(t *testing.T) {
	b, err := os.ReadFile("../collect/testdata/jsz-nats-1.json")
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	streams := r["data"].(map[string]any)["account_details"].([]any)[0].(map[string]any)["stream_detail"].([]any)
	cfg := streams[3].(map[string]any)["consumer_detail"].([]any)[0].(map[string]any)["config"].(map[string]any)
	cfg["deliver_policy"], cfg["ack_policy"] = "future", "future"
	if b, err = json.Marshal(r); err != nil {
		t.Fatal(err)
	}
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Jsz, b)
	snap.FinishedAt = t0
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s.DB(), `SELECT count(*) FROM consumer_config WHERE consumer = 'catalog-writer'`); got != 0 {
		t.Fatalf("catalog-writer's configuration stored %d times, want none", got)
	}
	want := `nats-1 sent consumer config.deliver_policy "future", which this nats-server version does not know, for 1 consumer; its configuration is not stored`
	if got := count(t, s.DB(), `SELECT count(*) FROM epochs WHERE list_contains(errors, ?)`, want); got != 1 {
		var notes string
		_ = s.DB().QueryRow(`SELECT string_agg(array_to_string(errors, ' | '), ' || ') FROM epochs`).Scan(&notes)
		t.Errorf("no note %q; notes: %s", want, notes)
	}
}
