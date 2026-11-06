package api_test

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
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
	f.Lookbacks = []api.CheckLookback{{Code: "server-014", Lookback: 6 * time.Hour}, {Code: "stream-006", Lookback: 12 * time.Hour}, {Code: "consumer-002", Lookback: time.Hour}}
	f.Samples = map[string]api.HistorySamples{}
	for _, w := range api.HistoryUseWindows(f) {
		f.Samples[w.ID] = api.HistorySamples{First: hNow.Add(-4 * time.Minute), Last: hNow.Add(-time.Minute)}
	}
	for _, u := range uses(f) {
		if u.GetEnough() || u.GetHas().AsDuration() != 3*time.Minute || u.GetEnoughAt() != nil {
			t.Errorf("stopped: %v", u)
		}
	}
	// The two forecasts have distinct configured windows; these are targets,
	// not min_span or proof that either entity's slopes are valid.
	if u := uses(f)["forecast:stream-006"]; u.GetNeeds().AsDuration() != 12*time.Hour {
		t.Error(u)
	}
	// A sparse sample span can be valid for RobustPerDay; continuity is not required.
	f.Samples["growth"] = api.HistorySamples{First: hNow.Add(-7 * time.Hour), Last: hNow.Add(-time.Minute)}
	if u := uses(f)["growth"]; !u.GetEnough() {
		t.Error(u)
	}
	// A fresh growing range may give a conditional earliest time.
	first, last := hNow.Add(-31*time.Minute), hNow.Add(-30*time.Second)
	f.Samples["growth"] = api.HistorySamples{First: first, Last: last}
	u := uses(f)["growth"]
	if u.GetEnough() || !u.GetEnoughAt().AsTime().Equal(hNow.Add(api.MinGrowthSpan-last.Sub(first))) {
		t.Error(u)
	}
	f.NewestEpochAt = hNow.Add(time.Second)
	if u := uses(f)["growth"]; u.GetEnoughAt() != nil {
		t.Error("future collection clock", u)
	}
	f.NewestEpochAt = last
	f.End = hNow.Add(-time.Hour)
	if u := uses(f)["growth"]; u.GetEnoughAt() != nil {
		t.Error(u)
	}
	f.End = hNow
	f.Start = hNow.Add(-6 * time.Hour)
	if u := uses(f)["growth"]; u.GetEnoughAt() != nil {
		t.Error("exclusive end cannot fit six-hour sample span", u)
	}
	f.Start = hNow.Add(-time.Hour)
	if u := uses(f)["growth"]; u.GetEnoughAt() != nil {
		t.Error(u)
	}
	f.Retention = 7 * 24 * time.Hour
	if u := uses(f)["accept_review"]; !u.GetNever() || u.GetEnough() || u.GetEnoughAt() != nil {
		t.Error(u)
	}
	f.Samples = nil
	for _, u := range uses(f) {
		if u.Has != nil || u.Enough || u.EnoughAt != nil {
			t.Error(u)
		}
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

// The times without collections and the listed gaps are cut at 200 each,
// with a total each: a flapping events connection cannot push a time
// without collections out.
func TestDataHistoryGapsCutPerClass(t *testing.T) {
	f := hFacts()
	f.Start = hNow.Add(-24 * time.Hour)
	f.HistoryStart = hAt(8, 0)
	f.Collections = []api.ReplayGap{noCollections(hAt(9, 0), hAt(9, 10), 1, 2)}
	for i := range 250 {
		at := hAt(10, 0).Add(time.Duration(i) * 30 * time.Second)
		f.Events = append(f.Events, hEvent(fmt.Sprint(2*i), "collector.disconnected", at, at, `{"connection":"sys"}`),
			hEvent(fmt.Sprint(2*i+1), "collector.reconnected", at.Add(15*time.Second), at, `{"connection":"sys"}`))
	}
	res := api.FinishDataHistory(f)
	var hatched, listed int
	for _, g := range res.GetGaps() {
		if g.GetKind() == v1.HistoryGapKind_HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED {
			listed++
		} else {
			hatched++
		}
	}
	if hatched != 2 || listed != api.MaxHistoryGaps || res.GetGapsTotal() != 2 || res.GetListedGapsTotal() != 250 {
		t.Errorf("%d hatched of %d, %d listed of %d", hatched, res.GetGapsTotal(), listed, res.GetListedGapsTotal())
	}
	if !slices.IsSortedFunc(res.GetGaps(), func(a, b *v1.HistoryGap) int { return b.GetStart().AsTime().Compare(a.GetStart().AsTime()) }) {
		t.Error("gaps not newest first")
	}
}

func TestDataHistoryBoundaryEvidence(t *testing.T) {
	const need = 5 * time.Minute
	const interval = 30 * time.Second
	lo := hNow.Add(-need)
	for _, tc := range []struct {
		name                string
		first, last, before time.Time
		interval, retention time.Duration
		enough              bool
	}{
		{"exact", lo, hNow, time.Time{}, interval, 0, true},
		{"phase", lo.Add(18 * time.Second), hNow, lo.Add(-12 * time.Second), interval, 0, true},
		{"exact tolerance", lo.Add(interval), hNow, lo.Add(-interval), interval, 0, true},
		{"past tolerance", lo.Add(interval + time.Microsecond), hNow, lo.Add(-time.Microsecond), interval, 0, false},
		{"old bracket", lo.Add(time.Second), hNow, lo.Add(-interval - time.Microsecond), interval, 0, false},
		{"new collector", lo.Add(time.Second), hNow, time.Time{}, interval, 0, false},
		{"large gap at boundary", lo.Add(2 * time.Minute), hNow, lo.Add(-time.Second), interval, 0, false},
		{"one sample", hNow, hNow, lo.Add(-time.Second), interval, 0, false},
		{"reversed", hNow, lo, time.Time{}, interval, 0, false},
		{"missing interval", lo.Add(time.Second), hNow, lo.Add(-time.Second), 0, 0, false},
		{"interval equals window", lo.Add(time.Second), hNow, lo.Add(-time.Second), need, 0, false},
		{"interval exceeds window", lo.Add(time.Second), hNow, lo.Add(-time.Second), 2 * need, 0, false},
		{"retention", lo.Add(time.Second), hNow, lo.Add(-time.Second), interval, need - time.Second, false},
		{"future sample", lo, hNow.Add(time.Second), lo.Add(-time.Second), interval, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := hFacts()
			f.NewestEpochAt = hNow
			f.Interval = tc.interval
			f.Retention = tc.retention
			f.Lookbacks = []api.CheckLookback{{Code: "server-014", Lookback: need}}
			f.Samples = map[string]api.HistorySamples{"forecast:server-014": {First: tc.first, Last: tc.last, Before: tc.before}}
			u := uses(f)["forecast:server-014"]
			if u.GetEnough() != tc.enough || u.GetHas().AsDuration() != max(tc.last.Sub(tc.first), 0) || u.GetEnoughAt() != nil {
				t.Fatal(u)
			}
			// Wall-clock time cannot lengthen a stopped collector's sample span.
			f.Now = hNow.Add(time.Hour)
			if got := uses(f)["forecast:server-014"]; got.GetEnough() != tc.enough && tc.name != "future sample" {
				t.Fatal(got)
			}
		})
	}
	// Growth has no boundary allowance, nor a countdown from zero/future span.
	for _, sample := range []api.HistorySamples{
		{First: hNow.Add(-api.MinGrowthSpan + time.Second), Last: hNow, Before: hNow.Add(-api.MinGrowthSpan - time.Second)},
		{First: hNow, Last: hNow}, {First: lo, Last: hNow.Add(time.Second)},
	} {
		f := hFacts()
		f.Samples = map[string]api.HistorySamples{"growth": sample}
		u := uses(f)["growth"]
		if u.GetEnough() {
			t.Fatal(u)
		}
		if (sample.First.Equal(sample.Last) || sample.Last.After(f.Now)) && u.GetEnoughAt() != nil {
			t.Fatal(u)
		}
	}
}

func TestDataHistoryNewestOmittedCollectionGap(t *testing.T) {
	f := hFacts()
	f.Start = hNow.Add(-24 * time.Hour)
	f.HistoryStart = f.Start
	for i := range 201 {
		from := f.Start.Add(time.Duration(i) * 5 * time.Minute)
		f.Collections = append(f.Collections, noCollections(from, from.Add(2*time.Minute), int64(2*i+1), int64(2*i+2)))
	}
	raw, err := protojson.Marshal(api.FinishDataHistory(f))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out["newestOmittedCollectionGap"]) == 0 {
		t.Fatalf("no omitted-gap boundary in %s", raw)
	}
	f.Collections = f.Collections[1:]
	raw, err = protojson.Marshal(api.FinishDataHistory(f))
	if err != nil {
		t.Fatal(err)
	}
	out = map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &out)
	if len(out["newestOmittedCollectionGap"]) != 0 {
		t.Fatal("exactly 200 must not claim omissions")
	}
}
