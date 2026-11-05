package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// The times ApplyNotify writes read back as given, whatever the session
// time zone: run with a non-UTC TZ as well.
func TestApplyNotifyTimes(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	at := t0.Add(123456 * time.Microsecond)
	addEpoch(t, s, 1, at)
	apply(t, s, 1, okRun("stream-001", frow("APP/jobs", "critical", "no quorum")))
	var outbox []OutboxRow
	for i := range 7 {
		outbox = append(outbox, OutboxRow{Channel: "ops", CreatedAt: at, Epoch: 1, Kind: "firing", FindingID: "stream-001:APP/jobs",
			DedupKey: fmt.Sprintf("k%d", i), Message: json.RawMessage(fmt.Sprintf(`{"kind":"firing","n":%d,"detail":"a < b & c"}`, i))})
	}
	outbox = append(outbox, OutboxRow{Channel: "ops", CreatedAt: at, Epoch: 1, Kind: "baseline", DedupKey: "b", Message: json.RawMessage(`{}`)})
	ch := NotifyChange{
		Channels: []NotifyChannel{{Name: "ops", BaselinedAt: at, BaselinedEpoch: 1, ReconciledAt: at, ReconciledEpoch: 1}},
		Upserts:  []NotifyState{{Channel: "ops", FindingID: "stream-001:APP/jobs", SinceEpoch: 1, Severity: "critical", State: NotifyFiring, SentAt: at, Epoch: 1}},
		Outbox:   outbox,
	}
	if err := s.ApplyNotify(ctx, ch); err != nil {
		t.Fatal(err)
	}
	chans, err := s.NotifyChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c := chans["ops"]; !c.BaselinedAt.Equal(at) || !c.ReconciledAt.Equal(at) {
		t.Errorf("channel %+v, want times %s", c, at)
	}
	in, err := s.NotifyInput(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if st := in.States["ops"]["stream-001:APP/jobs"]; !st.SentAt.Equal(at) || st.Severity != "critical" || !in.At.Equal(at) {
		t.Errorf("state %+v at %s, want %s", st, in.At, at)
	}
	rows, err := s.PendingOutbox(ctx, "ops", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(outbox) {
		t.Fatalf("%d rows", len(rows))
	}
	for i, r := range rows {
		if !r.CreatedAt.Equal(at) || r.DedupKey != outbox[i].DedupKey || r.FindingID != outbox[i].FindingID {
			t.Errorf("row %d: %+v", i, r)
		}
		if i > 0 && r.ID <= rows[i-1].ID {
			t.Errorf("IDs out of the queued order: %d after %d", r.ID, rows[i-1].ID)
		}
	}
	var m map[string]any
	if err := json.Unmarshal(rows[3].Message, &m); err != nil || m["n"] != 3.0 || m["detail"] != "a < b & c" {
		t.Errorf("message %s: %v", rows[3].Message, err)
	}
	// The change of an epoch already reconciled writes nothing.
	again := ch
	again.Channels = []NotifyChannel{{Name: "ops", ReconciledAt: at, ReconciledEpoch: 1}}
	if err := s.ApplyNotify(ctx, again); !errors.Is(err, ErrNotifyReconciled) {
		t.Errorf("second change for the epoch: %v", err)
	}
	if rows, _ := s.OutboxRows(ctx, ""); len(rows) != len(outbox) {
		t.Errorf("%d outbox rows after a refused change", len(rows))
	}
	// Deletes and upserts.
	if err := s.ApplyNotify(ctx, NotifyChange{
		Channels: []NotifyChannel{{Name: "ops", ReconciledAt: at.Add(time.Minute), ReconciledEpoch: 2}},
		Deletes:  []NotifyKey{{Channel: "ops", FindingID: "stream-001:APP/jobs"}},
		Upserts:  []NotifyState{{Channel: "ops", FindingID: "stream-001:APP/other", SinceEpoch: 1, Severity: "warning", State: NotifyFlapping, SentAt: at, Epoch: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	in, _ = s.NotifyInput(ctx, 1)
	if _, ok := in.States["ops"]["stream-001:APP/jobs"]; ok || in.States["ops"]["stream-001:APP/other"].State != NotifyFlapping {
		t.Errorf("states %+v", in.States)
	}
}

// The sweep keeps a sent row until the shorter of 7 days and retention,
// whatever the session time zone.
func TestSweepNotificationsAge(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	at := t0
	addEpoch(t, s, 1, at)
	if err := s.ApplyNotify(ctx, NotifyChange{
		Channels: []NotifyChannel{{Name: "ops", BaselinedAt: at, BaselinedEpoch: 1, ReconciledAt: at, ReconciledEpoch: 1}},
		Outbox: []OutboxRow{{Channel: "ops", CreatedAt: at, Epoch: 1, Kind: "baseline", DedupKey: "a", Message: json.RawMessage(`{}`)},
			{Channel: "ops", CreatedAt: at, Epoch: 1, Kind: "baseline", DedupKey: "b", Message: json.RawMessage(`{}`)}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE notify_outbox SET state = 'delivered' WHERE dedup_key = 'a'`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		now       time.Time
		retention time.Duration
		want      int
	}{
		{at.Add(7*24*time.Hour - time.Second), 30 * 24 * time.Hour, 2},
		{at.Add(2*time.Hour - time.Second), 2 * time.Hour, 2},
		{at.Add(2*time.Hour + time.Second), 2 * time.Hour, 1},
	} {
		if _, err := s.SweepNotifications(ctx, c.now, c.retention, []string{"ops"}); err != nil {
			t.Fatal(err)
		}
		if n := count(t, s.DB(), `SELECT count(*) FROM notify_outbox`); n != c.want {
			t.Errorf("sweep at %s with retention %s: %d rows, want %d", c.now, c.retention, n, c.want)
		}
	}
	res, err := s.SweepNotifications(ctx, at.Add(30*24*time.Hour), 30*24*time.Hour, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM notify_outbox`); n != 1 || res.Outbox != 0 {
		t.Errorf("pending row: %d rows left, %+v", n, res)
	}
	res, err = s.SweepNotifications(ctx, at, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Channels) != 1 || res.Channels[0] != "ops" || res.Dropped != 1 || count(t, s.DB(), `SELECT count(*) FROM notify_channels`) != 0 {
		t.Errorf("channel removed: %+v", res)
	}
}

// A reconcile reads the mutes in force at the epoch and those that ended
// since the oldest channel's last reconcile, not the mutes that ended
// before it.
func TestNotifyInputReadsRecentMutes(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	last, at := t0, t0.Add(time.Minute)
	addEpoch(t, s, 1, last)
	addEpoch(t, s, 2, at)
	if err := s.ApplyNotify(ctx, NotifyChange{Channels: []NotifyChannel{{Name: "ops", BaselinedAt: last, BaselinedEpoch: 1,
		ReconciledAt: last, ReconciledEpoch: 1}}}); err != nil {
		t.Fatal(err)
	}
	mk := func(reason string, created, expires time.Time) Mute {
		t.Helper()
		m, err := s.CreateMute(ctx, Mute{CheckCode: "stream-001", Reason: reason, MutedBy: "alice@example.com", CreatedAt: created, ExpiresAt: expires})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	mk("expired long ago", last.Add(-48*time.Hour), last.Add(-24*time.Hour))
	lifted := mk("lifted long ago", last.Add(-48*time.Hour), at.Add(time.Hour))
	if _, err := s.UnmuteMute(ctx, lifted.ID, "bob@example.com", last.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	mk("expired since the last reconcile", last.Add(-time.Hour), last.Add(30*time.Second))
	ended := mk("lifted since the last reconcile", last.Add(-time.Hour), at.Add(time.Hour))
	if _, err := s.UnmuteMute(ctx, ended.ID, "bob@example.com", last.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	mk("in force", last.Add(-time.Hour), at.Add(time.Hour))
	mk("created after the epoch", at.Add(time.Second), at.Add(time.Hour))
	in, err := s.NotifyInput(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range in.Mutes {
		got = append(got, m.Reason)
	}
	want := []string{"in force", "lifted since the last reconcile", "expired since the last reconcile"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("mutes read %q, want %q", got, want)
	}
}
