package storeprov_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/events"
)

// A client pod watch's failures and gaps make no Kubernetes gap in Data
// history: the Applications page flags them itself.
func TestDataHistoryLeavesOutClientPodWatches(t *testing.T) {
	st := historyStore(t, historyClock)
	day := historyClock.Truncate(24 * time.Hour)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	ev := func(when, received time.Time, kind string) events.Event {
		return events.Event{Time: when, ReceivedAt: received, Source: events.SourceCollector, Kind: kind, Summary: kind,
			Raw: json.RawMessage(`{"source":"client pods in shop","error":"Kubernetes API returned HTTP 403"}`)}
	}
	if err := st.WriteEvents(ctx, []events.Event{
		ev(at(12, 40), at(12, 45), "collector.k8s_gap"),
		ev(at(13, 10), at(13, 10), "collector.k8s_not_watching"),
		ev(at(13, 55), at(13, 55), "collector.k8s_not_watching"),
	}); err != nil {
		t.Fatal(err)
	}
	read := historyClient(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Retention: 14 * 24 * time.Hour,
		Clock: func() time.Time { return historyClock }})
	var got []string
	for _, g := range read(lastDay(historyClock)).GetGaps() {
		got = append(got, gapLine(g))
	}
	want := []string{
		"HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED 13:50:00-open",
		"HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED 13:20:00-13:24:00",
		"HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED 13:00:00-13:00:40",
		"HISTORY_GAP_KIND_NOT_CONNECTED 12:30:00-12:32:00",
		"HISTORY_GAP_KIND_NOT_RUNNING 12:00:00-12:03:00",
		"HISTORY_GAP_KIND_NO_COLLECTIONS 11:30:00-11:35:00",
		"HISTORY_GAP_KIND_BEFORE_HISTORY 14:00:30-11:00:00",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("gaps\n%v\nwant\n%v", got, want)
	}
}
