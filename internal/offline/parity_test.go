package offline

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
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
	var visit func(any)
	visit = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, c := range v {
				switch k {
				case "live", "collecting", "collection", "archive":
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
		{"ListEvents", `{"pageSize":1,"hideRoutine":false,"foldRepeats":false}`}, {"GetEvent", `{"id":"9007199254740993"}`},
		{"ListFindings", `{"includeMuted":true,"pageSize":1}`}, {"GetFinding", `{"id":"server-001:nats-0"}`},
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
