package offline

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/investigation"
	"github.com/shadone/nats-lens/internal/store"
)

// Only live collector fields and absent archiver status may differ. All captured
// measurements, scope, epoch/cursor, identity, history, and calculation fields match.
func withoutRuntime(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if info, ok := v.(map[string]any); ok && info["identityMode"] != nil {
		delete(info, "retention")
	}
	var visit func(any)
	visit = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, c := range v {
				switch k {
				case "live", "collecting", "collection", "archive", "nowFixed":
					delete(v, k)
				default:
					visit(c)
				}
			}
		case []any:
			for _, c := range v {
				visit(c)
			}
		}
	}
	visit(v)
	return v
}
func TestOfflineLiveParity(t *testing.T) {
	ctx := context.Background()
	path := closedFixture(t)
	// Add a genuine cumulative-counter pair; the exact increase exceeds JS's
	// safe integer range and must survive the same live/offline JSON path.
	w, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO epochs BY NAME SELECT * REPLACE (9007199254740992 AS epoch, TIMESTAMP '2026-10-17 14:28:30' AS started_at,TIMESTAMP '2026-10-17 14:28:31' AS finished_at) FROM epochs`,
		`UPDATE server_stats SET in_msgs=9007199254740993,out_msgs=42,in_bytes=9007199254740993,out_bytes=42`,
		`INSERT INTO server_stats BY NAME SELECT * REPLACE (9007199254740992 AS epoch,0 AS in_msgs,0 AS out_msgs,0 AS in_bytes,0 AS out_bytes) FROM server_stats`,
	} {
		if _, err = w.DB().ExecContext(ctx, q); err != nil {
			w.Close()
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	session, err := Open(ctx, investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	db, err := dataset.OpenDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st, err := store.NewReadView(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 17, 14, 29, 1, 0, time.UTC)
	liveProvider := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock }}
	server := httptest.NewServer(api.NewInvestigationHandler(&api.Handler{Systems: api.One("shop", "", liveProvider)}))
	defer server.Close()
	remote, err := investigation.NewRemote(investigation.Profile{Deployment: server.URL, Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ method, request string }{
		{"GetLensInfo", `{}`}, {"ListSystems", `{"stripBuckets":2}`}, {"ListMetrics", `{}`}, {"ListCheckDefinitions", `{}`},
		{"GetOverview", `{}`}, {"GetTimeSeries", `{"metric":"server.cpu_percent","entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0"},"maxPoints":10}`},
		{"GetTimeSeries", `{"metric":"server.cpu_percent","entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0"},"maxPoints":1,"includeObservations":true}`},
		{"GetTimeSeries", `{"metric":"server.in_msgs_rate","entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0"},"includeObservations":true,"maxPoints":1}`},
		{"GetTimeSeries", `{"metric":"cluster.in_msgs_rate","includeObservations":true,"maxPoints":1}`},
		{"ListMetricContributors", `{"metric":"server.cpu_percent","ranking":"METRIC_RANKING_MAXIMUM","descending":true,"limit":100}`},
		{"ListMetricContributors", `{"metric":"server.in_msgs_rate","ranking":"METRIC_RANKING_COUNTER_INCREASE","descending":true,"limit":100}`},
		{"ListMetricContributors", `{"metric":"stream.bytes","account":"APP","ranking":"METRIC_RANKING_MEAN","limit":100}`},
		{"GetTimeSeriesBatch", `{"queries":[ {"metric":"server.cpu_percent","entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0"},"includeObservations":true},{"metric":"cluster.cpu_percent","includeObservations":true}],"maxPoints":1}`},
		{"ListEvents", `{"pageSize":1,"hideRoutine":false,"foldRepeats":false}`}, {"GetEvent", `{"id":"9007199254740993"}`},
		{"ListFindings", `{"includeMuted":true,"pageSize":1}`}, {"GetFinding", `{"id":"server-001:nats-0"}`},
		{"ListFindings", `{"range":{"start":"2026-10-17T14:24:00Z","end":"2026-10-17T14:24:30Z"},"includeMuted":true}`},
		{"ListFindings", `{"range":{"start":"2026-10-17T14:25:30Z","end":"2026-10-17T14:26:00Z"},"includeMuted":true}`},
		{"ListFindingEpisodes", `{"findingId":"server-001:nats-0","pageSize":100}`},
		{"ListFindingEpisodes", `{"findingId":"server-001:nats-0","firstRetainedEpoch":"9007199254740989","includeTransitions":true,"pageSize":1,"range":{"start":"2026-10-17T14:24:00Z","end":"2026-10-17T14:24:30Z"}}`},
		{"ListEventSelection", `{"ids":["9007199254740993","9007199254740994"],"eventReadId":"9007199254740993","offset":1,"pageSize":1}`},
		{"GetEventBatch", `{"ids":["9007199254740993"],"eventReadId":"9007199254740993"}`},
		{"GetStatusTimeline", `{}`}, {"ListStreams", `{"account":"APP","limit":1}`}, {"GetStream", `{"account":"APP","stream":"orders"}`},
		{"GetCapacity", `{}`}, {"GetPlacement", `{}`}, {"GetDataHistory", `{}`}, {"GetSlowConsumerInvestigation", `{}`},
	}
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			live, le := remote.Query(ctx, c.method, []byte(c.request))
			offline, oe := session.Source.Query(ctx, c.method, []byte(c.request))
			if le != nil || oe != nil {
				t.Fatalf("live=%v offline=%v", le, oe)
			}
			if !reflect.DeepEqual(withoutRuntime(t, live), withoutRuntime(t, offline)) {
				t.Fatalf("live %s\noffline %s", live, offline)
			}
			if c.method == "GetTimeSeries" && strings.Contains(c.request, "in_msgs_rate") && !strings.Contains(string(offline), `"observedIncrease":"9007199254740993"`) && !strings.Contains(string(offline), `"observedIncrease":"18014398509481986"`) {
				t.Fatalf("exact counter evidence absent: %s", offline)
			}
			if c.method == "ListEvents" {
				var page struct {
					NextPageToken string `json:"nextPageToken"`
				}
				if err := json.Unmarshal(offline, &page); err != nil {
					t.Fatal(err)
				}
				if page.NextPageToken == "" {
					t.Fatal("fixture must page")
				}
				request, _ := json.Marshal(map[string]any{"pageSize": 1, "hideRoutine": false, "foldRepeats": false, "pageToken": page.NextPageToken})
				a, ae := remote.Query(ctx, c.method, request)
				b, be := session.Source.Query(ctx, c.method, request)
				if ae != nil || be != nil || !reflect.DeepEqual(withoutRuntime(t, a), withoutRuntime(t, b)) {
					t.Fatalf("continuation parity %s %s %v %v", a, b, ae, be)
				}
			}
		})
	}
	for _, c := range []struct{ method, request string }{
		{"ListServers", `{"system":"other"}`}, {"ListEvents", `{"pageSize":-1}`}, {"ListStreams", `{"pageToken":"bad"}`}, {"GetTimeSeries", `{"metric":"unknown"}`}, {"GetCapacity", `{"range":{"start":"2026-10-18T00:00:00Z","end":"2026-10-17T00:00:00Z"}}`},
	} {
		_, a := remote.Query(ctx, c.method, []byte(c.request))
		_, b := session.Source.Query(ctx, c.method, []byte(c.request))
		if a == nil || b == nil || a.Error() != b.Error() {
			t.Errorf("validation %s: %v / %v", c.method, a, b)
		}
	}
}

func TestOfflineMetricRankingReopenContinuation(t *testing.T) {
	ctx := context.Background()
	path := closedFixture(t)
	selection := investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second}
	a, err := Open(ctx, selection)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"metric": "server.cpu_percent", "ranking": "METRIC_RANKING_MAXIMUM", "descending": true, "limit": 1, "range": map[string]string{"start": "2026-10-17T14:00:00Z", "end": "2026-10-17T14:30:00Z"}}
	raw, _ := json.Marshal(request)
	data, err := a.Source.Query(ctx, "ListMetricContributors", raw)
	a.Close()
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		Next         string `json:"nextPageToken"`
		Contributors []struct {
			Entity struct {
				Key string `json:"key"`
			} `json:"entity"`
		} `json:"contributors"`
	}
	if err = json.Unmarshal(data, &first); err != nil {
		t.Fatal(err)
	}
	if first.Next == "" || len(first.Contributors) != 1 {
		t.Fatalf("missing traversal: %s", data)
	}
	b, err := Open(ctx, selection)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	request["pageToken"] = first.Next
	raw, _ = json.Marshal(request)
	data, err = b.Source.Query(ctx, "ListMetricContributors", raw)
	if err != nil {
		t.Fatal(err)
	}
	var second struct {
		Contributors []struct {
			Entity struct {
				Key string `json:"key"`
			} `json:"entity"`
		} `json:"contributors"`
	}
	if err = json.Unmarshal(data, &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Contributors) != 1 || first.Contributors[0].Entity.Key == second.Contributors[0].Entity.Key {
		t.Fatalf("offline continuation did not reopen: %s", data)
	}
}
