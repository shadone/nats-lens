package storeprov_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// An accepted finding reads as muted everywhere mutes are read: the
// findings list, the tally, the status timeline and /metrics.
func TestAcceptedFindingReadsAsMuted(t *testing.T) {
	st, set, _ := findingsStore(t)
	srv, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return now }})
	e := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: noLimitsKey}
	tally := func() *v1.FindingTally {
		return must(c.GetShellStatus(ctx, connect.NewRequest(&v1.GetShellStatusRequest{}))).GetTally()
	}
	if got := tally(); got.GetInfo() != 1 || got.GetMuted() != 0 {
		t.Fatalf("tally before = %v", got)
	}

	a := must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-005", Entity: e, Accept: true,
		Reason: "kept for the account's life", MutedBy: "ops@example.com", ExpiresAt: timestamppb.New(now.Add(180 * 24 * time.Hour))}))).GetMute()
	if !a.GetAccepted() || a.GrowthPerDay != nil || a.GetEndReason() != "" {
		t.Fatalf("acceptance = %v (the fixture spans 90 seconds: no growth)", a)
	}

	list := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour()})))
	for _, f := range list.GetFindings() {
		if f.GetCheckCode() == "stream-005" {
			t.Errorf("accepted finding listed with the unmuted ones: %v", f)
		}
	}
	all := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour(), IncludeMuted: true})))
	var found bool
	for _, f := range all.GetFindings() {
		if f.GetCheckCode() == "stream-005" {
			found = true
			if f.GetMute().GetId() != a.GetId() || !f.GetMute().GetAccepted() {
				t.Errorf("stream-005 mute = %v, want the acceptance", f.GetMute())
			}
		}
	}
	if !found {
		t.Error("accepted finding missing with include_muted")
	}

	if got := tally(); got.GetInfo() != 0 || got.GetMuted() != 1 {
		t.Errorf("tally after = %v, want the finding counted as muted", got)
	}

	r := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(now)}
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r, Bucket: durationpb.New(30 * time.Second)})))
	var row *v1.TimelineRow
	for _, x := range tl.GetRows() {
		if x.GetKey() == "stream-005" {
			row = x
		}
	}
	if row == nil || row.GetNow() != v1.Status_STATUS_MUTED {
		t.Errorf("timeline stream-005 = %v, want muted now", row)
	}

	body := scrape(t, srv.URL+"/metrics")
	for _, want := range []string{
		`nats_lens_finding{system="default",code="stream-005",entity_type="stream",entity_key="` + noLimitsKey + `",muted="true"} 1`,
		`nats_lens_findings_muted{system="default"} 1`,
		`nats_lens_findings_total{system="default",severity="info"} 0`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("/metrics missing %q", want)
		}
	}

	mutes := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{}))).GetMutes()
	if len(mutes) != 1 || !mutes[0].GetAccepted() {
		t.Errorf("mutes = %v", mutes)
	}
	un := must(c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: a.GetId(), UnmutedBy: "lead"}))).GetMute()
	if !un.GetAccepted() || un.GetUnmutedAt() == nil || un.GetEndReason() != "" {
		t.Errorf("lifted acceptance = %v", un)
	}
	if got := tally(); got.GetInfo() != 1 || got.GetMuted() != 0 {
		t.Errorf("tally after the lift = %v", got)
	}
}

var acceptNow = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

// growthFixture holds 433 epochs over the 9 days to acceptNow: the stream
// SHOP/order-archive grows 16 MiB a day, SHOP/new-stream exists for the
// last 21 hours only, and nats-0's JetStream storage grows 30 MiB a day.
func growthFixture(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB().Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	start := acceptNow.Add(-216 * time.Hour)
	exec(`INSERT INTO servers (server_id, server_name, first_epoch, last_epoch, first_seen_at, last_seen_at) VALUES ('NSERVER0', 'nats-0', 1, 433, ?, ?)`, start, acceptNow)
	for i := 0; i <= 432; i++ {
		at := start.Add(time.Duration(i) * 30 * time.Minute)
		days := at.Sub(start).Hours() / 24
		exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [])`, i+1, at, at)
		exec(`INSERT INTO server_stats (epoch, server_id, js_storage) VALUES (?, 'NSERVER0', ?)`, i+1, int64(1<<30+days*30*(1<<20)))
		// A follower with other bytes: the leader's are the ones read.
		exec(`INSERT INTO stream_stats (epoch, server_id, account, stream, bytes, is_leader) VALUES (?, 'NSERVER0', 'SHOP', 'order-archive', ?, true),
  (?, 'NSERVER1', 'SHOP', 'order-archive', 1, false)`, i+1, int64(100*(1<<20)+days*16*(1<<20)), i+1)
		if i >= 390 {
			exec(`INSERT INTO stream_stats (epoch, server_id, account, stream, bytes, is_leader) VALUES (?, 'NSERVER0', 'SHOP', 'new-stream', ?, true)`,
				i+1, int64(i)*(1<<20))
		}
	}
	return st
}

// The server records the entity's growth at acceptance: a stream's leader
// bytes and a server's JetStream storage, the week's growth by
// WeeklyGrowth; nothing for other entity types or under 8 days of data.
func TestAcceptanceRecordsGrowth(t *testing.T) {
	st := growthFixture(t)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Minute, Live: true, Clock: func() time.Time { return acceptNow }})
	accept := func(code string, typ v1.EntityType, key string) *v1.Mute {
		t.Helper()
		return must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: code, Accept: true,
			Entity: &v1.EntityRef{Type: typ, Key: key}, Reason: "by design", MutedBy: "ops@example.com",
			ExpiresAt: timestamppb.New(acceptNow.Add(90 * 24 * time.Hour))}))).GetMute()
	}
	const mib = 1 << 20
	if m := accept("stream-005", v1.EntityType_ENTITY_TYPE_STREAM, "SHOP/order-archive"); m.GrowthPerDay == nil || *m.GrowthPerDay != 16*mib {
		t.Errorf("stream growth = %v, want 16 MiB/day", m.GrowthPerDay)
	}
	// A check without the growth review records none, even for a growing stream.
	if m := accept("stream-018", v1.EntityType_ENTITY_TYPE_STREAM, "SHOP/order-archive"); m.GrowthPerDay != nil || m.GetGrowthReview() {
		t.Errorf("stream-018 growth = %v, review %v; want none", m.GrowthPerDay, m.GetGrowthReview())
	}
	if m := accept("stream-005", v1.EntityType_ENTITY_TYPE_STREAM, "SHOP/new-stream"); m.GrowthPerDay != nil {
		t.Errorf("growth of a stream with 21 hours of data = %v, want none", *m.GrowthPerDay)
	}
	if m := accept("consumer-016", v1.EntityType_ENTITY_TYPE_CONSUMER, "SHOP/order-archive/worker"); m.GrowthPerDay != nil {
		t.Errorf("consumer growth = %v, want none", *m.GrowthPerDay)
	}
	// A plain mute records none, even for a growing stream.
	m := must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-006",
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "SHOP/order-archive"}, Reason: "r", MutedBy: "op",
		ExpiresAt: timestamppb.New(acceptNow.Add(time.Hour))}))).GetMute()
	if m.GetAccepted() || m.GrowthPerDay != nil {
		t.Errorf("plain mute = %v", m)
	}
	// The acceptance is refused without an entity, and past 365 days.
	for name, req := range map[string]*v1.MuteFindingRequest{
		"no entity": {CheckCode: "stream-005", Accept: true, Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(acceptNow.Add(48 * time.Hour))},
		"366 days": {CheckCode: "stream-005", Accept: true, Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(acceptNow.Add(366 * 24 * time.Hour)),
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "SHOP/order-archive"}},
	} {
		if _, err := c.MuteFinding(ctx, connect.NewRequest(req)); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
}
