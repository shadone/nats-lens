package storeprov_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// historyStore holds three hours of epochs, 30 seconds apart, from 11:00
// to 14:00 of the day of clock (UTC), with one gap of each kind:
//   - no collections from 11:30 to 11:35, without an event;
//   - nats-lens not running from 12:00 to 12:03 (collector.gap), started
//     again at 12:03;
//   - the collector's connection closed from 12:30:05 to 12:31:55, no
//     collection from 12:30 to 12:32;
//   - the sys events connection lost from 13:00:00 to 13:00:40;
//   - Kubernetes not watched from 13:20 to 13:24 (collector.k8s_gap), and
//     again from 13:50 on (collector.k8s_not_watching, still open).
//
// The epoch at 13:40 is partial. Two findings fire from the history start
// (one first seen 20 seconds after it, one whose condition began before
// it) and one from later.
func historyStore(t *testing.T, clock time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	day := clock.Truncate(24 * time.Hour)
	at := func(h, m, s int) time.Time {
		return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	holes := [][2]time.Time{{at(11, 30, 0), at(11, 35, 0)}, {at(12, 0, 0), at(12, 3, 0)}, {at(12, 30, 0), at(12, 32, 0)}}
	r.server("S0", "nats-0")
	n := int64(0)
	for e := at(11, 0, 0); !e.After(at(14, 0, 0)); e = e.Add(30 * time.Second) {
		if slices.ContainsFunc(holes, func(h [2]time.Time) bool { return e.After(h[0]) && e.Before(h[1]) }) {
			continue
		}
		n++
		r.epoch(n, e, "S0")
		r.add("server_stats", map[string]any{"epoch": n, "server_id": "S0", "connections": 5 + n%3})
	}
	if _, err := st.DB().Exec(`UPDATE epochs SET partial = true WHERE started_at = ?`, at(13, 40, 0)); err != nil {
		t.Fatal(err)
	}
	ev := func(when, received time.Time, kind, conn string) events.Event {
		raw := json.RawMessage(`{}`)
		if conn != "" {
			raw = json.RawMessage(`{"connection":"` + conn + `"}`)
		}
		return events.Event{Time: when, ReceivedAt: received, Source: events.SourceCollector, Kind: kind, Summary: kind + " " + conn, Raw: raw}
	}
	if err := st.WriteEvents(ctx, []events.Event{
		ev(at(10, 59, 50), at(10, 59, 50), "collector.started", ""),
		ev(at(12, 0, 0), at(12, 3, 0), "collector.gap", ""),
		ev(at(12, 3, 0), at(12, 3, 0), "collector.started", ""),
		ev(at(12, 30, 5), at(12, 30, 5), "collector.connection_closed", "collector"),
		ev(at(12, 31, 55), at(12, 31, 55), "collector.reconnected", "collector"),
		ev(at(13, 0, 0), at(13, 0, 0), "collector.disconnected", "sys"),
		ev(at(13, 0, 40), at(13, 0, 40), "collector.reconnected", "sys"),
		ev(at(13, 20, 0), at(13, 24, 0), "collector.k8s_gap", ""),
		ev(at(13, 50, 0), at(13, 50, 0), "collector.k8s_not_watching", ""),
	}); err != nil {
		t.Fatal(err)
	}
	finding := func(id string, since time.Time, condition any) {
		r.add("findings", map[string]any{"id": id, "check_code": "stream-005", "entity_type": "stream", "entity_key": id, "severity": "info",
			"state": "firing", "last_transition": "firing", "since": since, "since_epoch": 1, "transitioned_at": since, "last_seen_epoch": n,
			"updated_epoch": n, "detail": "no limits", "condition_since": condition})
	}
	finding("APP/a", at(11, 0, 20), nil)
	finding("APP/b", at(11, 30, 0), at(9, 0, 0))
	finding("APP/c", at(13, 0, 0), nil)
	return st
}

// historyClock is the clock of the history fixtures: 30 seconds after the
// last epoch.
var historyClock = time.Date(2026, 10, 17, 14, 0, 30, 0, time.UTC)

func historyClient(t *testing.T, p *storeprov.Provider) func(r *v1.TimeRange) *v1.GetDataHistoryResponse {
	t.Helper()
	_, c := apitest.Serve(t, p)
	return func(r *v1.TimeRange) *v1.GetDataHistoryResponse {
		t.Helper()
		return must(c.GetDataHistory(ctx, connect.NewRequest(&v1.GetDataHistoryRequest{Range: r})))
	}
}

func lastDay(clock time.Time) *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(clock.Add(-24 * time.Hour)), End: timestamppb.New(clock)}
}

// gapLine is a gap as text, for comparing lists.
func gapLine(g *v1.HistoryGap) string {
	end := "open"
	if g.GetEnd() != nil {
		end = g.GetEnd().AsTime().UTC().Format("15:04:05")
	}
	return fmt.Sprintf("%s %s-%s", g.GetKind(), g.GetStart().AsTime().UTC().Format("15:04:05"), end)
}

// The store reads every kind of gap, the coverage of the range and the
// history's starts, the same in any session time zone.
func TestDataHistoryFromStore(t *testing.T) {
	st := historyStore(t, historyClock)
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Retention: 14 * 24 * time.Hour,
		Clock: func() time.Time { return historyClock }}
	read := historyClient(t, p)
	want := []string{
		"HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED 13:50:00-open",
		"HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED 13:20:00-13:24:00",
		"HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED 13:00:00-13:00:40",
		"HISTORY_GAP_KIND_NOT_CONNECTED 12:30:00-12:32:00",
		"HISTORY_GAP_KIND_NOT_RUNNING 12:00:00-12:03:00",
		"HISTORY_GAP_KIND_NO_COLLECTIONS 11:30:00-11:35:00",
		"HISTORY_GAP_KIND_BEFORE_HISTORY 14:00:30-11:00:00",
	}
	day := historyClock.Truncate(24 * time.Hour)
	for _, zone := range []string{"UTC", "Asia/Kolkata", "America/Los_Angeles"} {
		if _, err := st.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
			t.Fatal(err)
		}
		res := read(lastDay(historyClock))
		var got []string
		for _, g := range res.GetGaps() {
			got = append(got, gapLine(g))
		}
		if !slices.Equal(got, want) || res.GetGapsTotal() != 4 || res.GetListedGapsTotal() != 3 {
			t.Fatalf("%s: gaps\n%v\nwant\n%v", zone, got, want)
		}
		gs := res.GetGaps()
		if g := gs[3]; g.GetEventId() == "" || g.GetBeforeEpoch() == 0 || g.GetAfterEpoch() != g.GetBeforeEpoch()+1 ||
			!g.GetBeforeEpochAt().AsTime().Equal(g.GetStart().AsTime()) {
			t.Errorf("%s: not connected gap %v: want its event and the collections either side", zone, g)
		}
		if g := gs[2]; g.GetConnection() != "sys" || g.GetEventId() == "" {
			t.Errorf("%s: events gap %v: want the sys connection and its event", zone, g)
		}
		c := res.GetCoverage()
		if !c.GetCoveredFrom().AsTime().Equal(day.Add(11*time.Hour)) || !c.GetCoveredTo().AsTime().Equal(historyClock) ||
			c.GetEpochs() != 361-9-5-3 || c.GetExpectedEpochs() != 361 || c.GetPartialEpochs() != 1 ||
			c.GetWithoutData().AsDuration() != 10*time.Minute {
			t.Errorf("%s: coverage %v", zone, c)
		}
		if !res.GetHistoryStart().AsTime().Equal(day.Add(11*time.Hour)) || !res.GetNewestEpochAt().AsTime().Equal(day.Add(14*time.Hour)) ||
			!res.GetEventsStart().AsTime().Equal(day.Add(10*time.Hour+59*time.Minute+50*time.Second)) {
			t.Errorf("%s: starts %v %v %v", zone, res.GetHistoryStart(), res.GetNewestEpochAt(), res.GetEventsStart())
		}
		if res.GetFindingsAtStart() != 2 || res.GetRetention().AsDuration() != 14*24*time.Hour ||
			!res.GetRetentionFrom().AsTime().Equal(historyClock.Add(-14*24*time.Hour)) {
			t.Errorf("%s: findings at start %d, retention %v from %v", zone, res.GetFindingsAtStart(), res.GetRetention(), res.GetRetentionFrom())
		}
		if a := res.GetArchive(); a.GetEnabled() {
			t.Errorf("%s: archive %v without an archiver", zone, a)
		}
	}
}

// A gap that began before the range keeps its start; a range inside a
// gap is that gap alone, without collections.
func TestDataHistoryGapBeforeRange(t *testing.T) {
	read := historyClient(t, &storeprov.Provider{St: historyStore(t, historyClock), Interval: 30 * time.Second, Live: true,
		Clock: func() time.Time { return historyClock }})
	day := historyClock.Truncate(24 * time.Hour)
	res := read(&v1.TimeRange{Start: timestamppb.New(day.Add(12*time.Hour + time.Minute)), End: timestamppb.New(day.Add(12*time.Hour + 2*time.Minute))})
	if len(res.GetGaps()) != 1 || gapLine(res.GetGaps()[0]) != "HISTORY_GAP_KIND_NOT_RUNNING 12:00:00-open" {
		t.Errorf("inside the gap: %v", res.GetGaps())
	}
	if c := res.GetCoverage(); c.GetEpochs() != 0 || c.GetWithoutData().AsDuration() != time.Minute || c.GetExpectedEpochs() != 2 {
		t.Errorf("coverage inside the gap: %v", c)
	}
	res = read(&v1.TimeRange{Start: timestamppb.New(day.Add(11*time.Hour + 32*time.Minute)), End: timestamppb.New(day.Add(11*time.Hour + 50*time.Minute))})
	if len(res.GetGaps()) != 1 || gapLine(res.GetGaps()[0]) != "HISTORY_GAP_KIND_NO_COLLECTIONS 11:30:00-11:35:00" {
		t.Errorf("from inside the gap: %v", res.GetGaps())
	}
	if c := res.GetCoverage(); c.GetWithoutData().AsDuration() != 3*time.Minute {
		t.Errorf("without data from inside the gap: %v", c.GetWithoutData())
	}
}

// Without any epoch the whole range is before the history, and nothing is
// counted as covered.
func TestDataHistoryEmptyStore(t *testing.T) {
	read := historyClient(t, &storeprov.Provider{St: emptyStore(t), Interval: 30 * time.Second, Live: true,
		Clock: func() time.Time { return historyClock }})
	res := read(lastDay(historyClock))
	if len(res.GetGaps()) != 1 || res.GetGaps()[0].GetKind() != v1.HistoryGapKind_HISTORY_GAP_KIND_BEFORE_HISTORY || res.GetGaps()[0].GetEnd() != nil {
		t.Errorf("gaps %v", res.GetGaps())
	}
	if c := res.GetCoverage(); c.GetCoveredFrom() != nil || c.GetWithoutData() != nil || c.GetEpochs() != 0 || res.GetHistoryStart() != nil {
		t.Errorf("coverage %v, history start %v", c, res.GetHistoryStart())
	}
	for _, u := range res.GetUses() {
		if u.GetHas() != nil || u.GetEnough() || u.GetEnoughAt() != nil {
			t.Errorf("use %v without history", u)
		}
	}
}

// The archive's state is the archiver's, passed through as it is.
func TestDataHistoryArchiveState(t *testing.T) {
	hour := historyClock.Truncate(time.Hour)
	state := api.ArchiveState{Location: "/var/lib/natslens/archive", Retention: 90 * 24 * time.Hour, OldestHour: hour.Add(-3 * time.Hour),
		NewestHour: hour, Hours: 4, LastExportAt: historyClock.Add(-time.Minute), LastError: "disk full", LastErrorAt: historyClock,
		ListedAt: hour.Add(5 * time.Minute)}
	read := historyClient(t, &storeprov.Provider{St: historyStore(t, historyClock), Interval: 30 * time.Second, Live: true,
		Clock: func() time.Time { return historyClock }, ArchiveStatus: func() api.ArchiveState { return state }})
	a := read(lastDay(historyClock)).GetArchive()
	if !a.GetEnabled() || a.GetLocation() != state.Location || a.GetArchiveRetention().AsDuration() != state.Retention || a.GetHours() != 4 ||
		!a.GetOldestHour().AsTime().Equal(state.OldestHour) || !a.GetNewestHour().AsTime().Equal(hour) || a.GetLastError() != "disk full" ||
		!a.GetLastErrorAt().AsTime().Equal(historyClock) || !a.GetListedAt().AsTime().Equal(state.ListedAt) || a.GetNewestFinal() {
		t.Errorf("archive %v", a)
	}
}

// The read takes three queries, none per epoch: 14 days of epochs at a
// 30-second interval stay well inside the call's time limit.
func TestDataHistoryFourteenDays(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 40,320 epochs")
	}
	st := emptyStore(t)
	first := historyClock.Add(-30 * time.Second).Add(-(40320 - 1) * 30 * time.Second)
	if _, err := st.DB().Exec(`INSERT INTO epochs SELECT i, make_timestamp(?::BIGINT + (i - 1) * 30000000),
  make_timestamp(?::BIGINT + (i - 1) * 30000000), 0, 1, 1, [], [], [], 0, i % 500 = 0, [], NULL FROM range(1, 40321) t(i)`,
		first.UnixMicro(), first.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	// Five times without collections of 10 minutes.
	if _, err := st.DB().Exec(`DELETE FROM epochs WHERE epoch > 8000 AND epoch % 8000 BETWEEN 1 AND 20`); err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Retention: 14 * 24 * time.Hour,
		Clock: func() time.Time { return historyClock }}
	r := &v1.TimeRange{Start: timestamppb.New(historyClock.Add(-14 * 24 * time.Hour)), End: timestamppb.New(historyClock)}
	began := time.Now()
	f, err := p.DataHistory(ctx, &v1.GetDataHistoryRequest{Range: r})
	took := time.Since(began)
	if err != nil {
		t.Fatal(err)
	}
	res := api.FinishDataHistory(f)
	t.Logf("14 days, %d epochs, %d gaps: read in %v", res.GetCoverage().GetEpochs(), res.GetGapsTotal(), took)
	if res.GetCoverage().GetEpochs() != 40320-5*20 || res.GetGapsTotal() != 5 {
		t.Errorf("coverage %v, %d gaps", res.GetCoverage(), res.GetGapsTotal())
	}
	if took > 10*time.Second {
		t.Errorf("read took %v", took)
	}
}
