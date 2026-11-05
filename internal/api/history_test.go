package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var hNow = time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)

// hAt is a time on hNow's day.
func hAt(h, m int) time.Time { return time.Date(2026, 10, 17, h, m, 0, 0, time.UTC) }

func hEvent(id, kind string, at, received time.Time, raw string) *v1.Event {
	return &v1.Event{Id: id, Kind: kind, Time: timestamppb.New(at), ReceivedAt: timestamppb.New(received), Raw: raw,
		Source: v1.EventSource_EVENT_SOURCE_COLLECTOR}
}

// hFacts are facts of a history from 08:00 over the range 06:00 to now.
func hFacts() *api.HistoryFacts {
	return &api.HistoryFacts{Start: hAt(6, 0), End: hNow, Now: hNow, Interval: 30 * time.Second, Retention: 14 * 24 * time.Hour,
		HistoryStart: hAt(8, 0), NewestEpochAt: hNow.Add(-30 * time.Second)}
}

func noCollections(from, to time.Time, before, after int64) api.ReplayGap {
	return api.ReplayGap{Kind: api.GapCollections, Start: from, End: to, BeforeEpoch: before, BeforeAt: from, AfterEpoch: after, AfterAt: to}
}

func lines(res *v1.GetDataHistoryResponse) []string {
	var out []string
	for _, g := range res.GetGaps() {
		end := "open"
		if g.GetEnd() != nil {
			end = g.GetEnd().AsTime().Format("15:04")
		}
		out = append(out, fmt.Sprintf("%s %s-%s %s", g.GetKind(), g.GetStart().AsTime().Format("15:04"), end, g.GetEventId()))
	}
	return out
}

// Gaps that overlap are one, labelled not running before not connected
// before no collections, with the collections either side; events and
// Kubernetes gaps stay apart, as they are not drawn on charts.
func TestDataHistoryGapOverlapAndOrder(t *testing.T) {
	f := hFacts()
	f.Collections = []api.ReplayGap{
		noCollections(hAt(9, 0), hAt(9, 10), 100, 101),
		noCollections(hAt(10, 0), hAt(10, 20), 200, 201),
		noCollections(hAt(11, 0), hAt(11, 5), 300, 301),
	}
	f.Events = []*v1.Event{
		// Not running inside the first no-collections gap.
		hEvent("1", "collector.gap", hAt(9, 2), hAt(9, 8), `{}`),
		hEvent("2", "collector.started", hAt(9, 8), hAt(9, 8), `{}`),
		// Not connected and not running overlap the second: not running
		// labels it.
		hEvent("3", "collector.not_connected", hAt(10, 0), hAt(10, 0), `{"connection":"collector"}`),
		hEvent("4", "collector.gap", hAt(10, 10), hAt(10, 20), `{}`),
		hEvent("5", "collector.started", hAt(10, 20), hAt(10, 20), `{}`),
		// The third is not connected.
		hEvent("6", "collector.connection_closed", hAt(11, 1), hAt(11, 1), `{"connection":"collector"}`),
		hEvent("7", "collector.reconnected", hAt(11, 4), hAt(11, 4), `{"connection":"collector"}`),
		// An events connection lost for a minute inside it; a failover of 5
		// seconds is no gap.
		hEvent("8", "collector.disconnected", hAt(11, 2), hAt(11, 2), `{"connection":"sys"}`),
		hEvent("9", "collector.reconnected", hAt(11, 3), hAt(11, 3), `{"connection":"sys"}`),
		hEvent("10", "collector.disconnected", hAt(12, 0), hAt(12, 0), `{"connection":"sys"}`),
		hEvent("11", "collector.reconnected", hAt(12, 0).Add(5*time.Second), hAt(12, 0), `{"connection":"sys"}`),
		hEvent("12", "collector.k8s_gap", hAt(12, 30), hAt(12, 34), `{}`),
	}
	res := api.FinishDataHistory(f)
	want := []string{
		"HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED 12:30-12:34 12",
		"HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED 11:02-11:03 8",
		"HISTORY_GAP_KIND_NOT_CONNECTED 11:00-11:05 6",
		"HISTORY_GAP_KIND_NOT_RUNNING 10:00-10:20 4",
		"HISTORY_GAP_KIND_NOT_RUNNING 09:00-09:10 1",
		"HISTORY_GAP_KIND_BEFORE_HISTORY 06:00-08:00 ",
	}
	if got := lines(res); !slices.Equal(got, want) {
		t.Errorf("gaps\n%v\nwant\n%v", got, want)
	}
	if g := res.GetGaps()[3]; g.GetBeforeEpoch() != 200 || g.GetAfterEpoch() != 201 || !g.GetAfterEpochAt().AsTime().Equal(hAt(10, 20)) {
		t.Errorf("merged gap's collections: %v", g)
	}
	if g := res.GetGaps()[1]; g.GetConnection() != "sys" {
		t.Errorf("events gap connection %q", g.GetConnection())
	}
	// Time without data: 10 + 20 + 5 minutes; the events and Kubernetes
	// gaps had collections.
	if c := res.GetCoverage(); c.GetWithoutData().AsDuration() != 35*time.Minute || !c.GetCoveredFrom().AsTime().Equal(hAt(8, 0)) ||
		c.GetExpectedEpochs() != int64(6*time.Hour/(30*time.Second)) {
		t.Errorf("coverage %v", c)
	}
}

// A gap that began before the range keeps its start, and counts towards
// the time without data only inside the range; one still open at the
// range's end has no end.
func TestDataHistoryGapAcrossTheRange(t *testing.T) {
	f := hFacts()
	f.Start, f.End = hAt(9, 0), hAt(12, 0)
	f.Collections = []api.ReplayGap{noCollections(hAt(8, 50), hAt(9, 20), 10, 11), {Kind: api.GapCollections, Start: hAt(11, 30),
		BeforeEpoch: 50, BeforeAt: hAt(11, 30)}}
	f.Events = []*v1.Event{hEvent("1", "collector.connection_closed", hAt(11, 31), hAt(11, 31), `{"connection":"collector"}`),
		hEvent("2", "collector.k8s_not_watching", hAt(11, 40), hAt(11, 40), `{}`)}
	res := api.FinishDataHistory(f)
	want := []string{
		"HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED 11:40-open 2",
		"HISTORY_GAP_KIND_NOT_CONNECTED 11:30-open 1",
		"HISTORY_GAP_KIND_NO_COLLECTIONS 08:50-09:20 ",
	}
	if got := lines(res); !slices.Equal(got, want) {
		t.Errorf("gaps\n%v\nwant\n%v", got, want)
	}
	if c := res.GetCoverage(); c.GetWithoutData().AsDuration() != 50*time.Minute {
		t.Errorf("without data %v, want 20 min of the first and 30 of the open one", c.GetWithoutData())
	}
	// A gap that ended after the range is open at its end.
	f.Events = nil
	f.Collections[1].End = hAt(12, 30)
	if got := lines(api.FinishDataHistory(f))[0]; got != "HISTORY_GAP_KIND_NO_COLLECTIONS 11:30-open " {
		t.Errorf("gap ending after the range: %s", got)
	}
}

// At most 200 gaps are listed, the newest; the total counts all.
func TestDataHistoryGapsCut(t *testing.T) {
	f := hFacts()
	f.Start = hNow.Add(-24 * time.Hour)
	f.HistoryStart = f.Start
	for i := range 201 {
		from := f.Start.Add(time.Duration(i) * 5 * time.Minute)
		f.Collections = append(f.Collections, noCollections(from, from.Add(2*time.Minute), int64(2*i+1), int64(2*i+2)))
	}
	res := api.FinishDataHistory(f)
	if len(res.GetGaps()) != api.MaxHistoryGaps || res.GetGapsTotal() != 201 {
		t.Fatalf("%d gaps of %d", len(res.GetGaps()), res.GetGapsTotal())
	}
	if first, last := res.GetGaps()[0], res.GetGaps()[199]; first.GetBeforeEpoch() != 401 || last.GetBeforeEpoch() != 3 {
		t.Errorf("kept %d to %d, want the newest 200", first.GetBeforeEpoch(), last.GetBeforeEpoch())
	}
}

// uses returns the uses by ID.
func uses(f *api.HistoryFacts) map[string]*v1.HistoryUse {
	out := map[string]*v1.HistoryUse{}
	for _, u := range api.FinishDataHistory(f).GetUses() {
		out[u.GetId()] = u
	}
	return out
}

// Each use says whether it has enough, when it will, or that the
// retention never lets it.
func TestDataHistoryUses(t *testing.T) {
	f := hFacts()
	f.Start = hNow.Add(-24 * time.Hour)
	f.HistoryStart = hNow.Add(-31 * time.Minute)
	f.Lookbacks = []api.CheckLookback{{Code: "server-014", Lookback: 6 * time.Hour}, {Code: "consumer-002", Lookback: 30 * time.Minute},
		{Code: "server-004", Lookback: time.Hour}}
	u := uses(f)
	if g := u["growth"]; g.GetEnough() || g.GetHas().AsDuration() != 31*time.Minute || !g.GetEnoughAt().AsTime().Equal(f.HistoryStart.Add(6*time.Hour)) {
		t.Errorf("growth %v: want 31 min, enough 6 h after the start", g)
	}
	if g := u["forecast:server-014"]; g.GetEnough() || g.GetCheckCode() != "server-014" || g.GetNeeds().AsDuration() != 6*time.Hour {
		t.Errorf("forecast %v", g)
	}
	if _, ok := u["forecast:stream-006"]; ok {
		t.Error("a forecast whose check is not in the set is listed")
	}
	// Only the checks still short are listed.
	if _, ok := u["lookback:consumer-002"]; ok {
		t.Error("consumer-002 has enough and is listed")
	}
	if g := u["lookback:server-004"]; g == nil || !g.GetEnoughAt().AsTime().Equal(f.HistoryStart.Add(time.Hour)) {
		t.Errorf("server-004 %v", g)
	}
	if g := u["accept_review"]; g.GetEnough() || g.GetNever() || !g.GetEnoughAt().AsTime().Equal(f.HistoryStart.Add(api.AcceptRetention)) {
		t.Errorf("accept review %v", g)
	}
	// A retention shorter than the need never has enough.
	f.Retention = 7 * 24 * time.Hour
	if g := uses(f)["accept_review"]; !g.GetNever() || g.GetEnough() || g.GetEnoughAt() != nil {
		t.Errorf("accept review under 7 days: %v", g)
	}
	// Enough: no time to wait.
	f.HistoryStart = hNow.Add(-10 * 24 * time.Hour)
	f.Retention = 14 * 24 * time.Hour
	u = uses(f)
	for _, id := range []string{"growth", "accept_review", "day", "replay", "forecast:server-014"} {
		if g := u[id]; !g.GetEnough() || g.GetEnoughAt() != nil || g.GetNever() {
			t.Errorf("%s with 10 days: %v", id, g)
		}
	}
	if len(u) != 5 {
		t.Errorf("uses %v: want no lookback row once every check has enough", u)
	}
	// Growth reads the range: a 1-hour range never holds 6 hours, and an
	// ended range does not grow.
	f.Start = hNow.Add(-time.Hour)
	if g := uses(f)["growth"]; g.GetEnough() || g.GetEnoughAt() != nil || g.GetHas().AsDuration() != time.Hour {
		t.Errorf("growth over 1 h: %v", g)
	}
	f.Start, f.End, f.HistoryStart = hNow.Add(-30*time.Hour), hNow.Add(-27*time.Hour), hNow.Add(-28*time.Hour)
	if g := uses(f)["growth"]; g.GetEnough() || g.GetEnoughAt() != nil {
		t.Errorf("growth over an ended range: %v", g)
	}
}

// Before the first collection the whole range is before the history, and
// nothing is covered or has history.
func TestDataHistoryWithoutHistory(t *testing.T) {
	f := hFacts()
	f.HistoryStart, f.NewestEpochAt = time.Time{}, time.Time{}
	res := api.FinishDataHistory(f)
	if got := lines(res); !slices.Equal(got, []string{"HISTORY_GAP_KIND_BEFORE_HISTORY 06:00-open "}) {
		t.Errorf("gaps %v", got)
	}
	if c := res.GetCoverage(); c.GetCoveredFrom() != nil || c.GetWithoutData() != nil {
		t.Errorf("coverage %v", c)
	}
	if res.GetArchive().GetEnabled() || res.GetArchive().GetListedAt() != nil {
		t.Errorf("archive %v without an archiver", res.GetArchive())
	}
}

// The collector's own connection is not connected from its loss to its
// reconnect or to the next start; other connections' events are not.
func TestCollectorConnectionGaps(t *testing.T) {
	evs := []*v1.Event{
		hEvent("1", "collector.not_connected", hAt(9, 0), hAt(9, 0), `{"connection":"sys"}`),
		hEvent("2", "collector.not_connected", hAt(9, 1), hAt(9, 1), `{"connection":"collector"}`),
		hEvent("3", "collector.connection_closed", hAt(9, 2), hAt(9, 2), `{"connection":"collector"}`),
		hEvent("4", "collector.started", hAt(9, 5), hAt(9, 5), `{}`),
		hEvent("5", "collector.connection_closed", hAt(10, 0), hAt(10, 0), `{"connection":"collector"}`),
	}
	gs := api.CollectorConnectionGaps(evs)
	if len(gs) != 2 || !gs[0].Start.Equal(hAt(9, 1)) || !gs[0].End.Equal(hAt(9, 5)) || gs[0].Event.GetId() != "2" ||
		!gs[1].Start.Equal(hAt(10, 0)) || !gs[1].End.IsZero() {
		t.Errorf("gaps %+v", gs)
	}
}
