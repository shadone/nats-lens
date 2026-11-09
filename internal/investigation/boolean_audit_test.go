package investigation

import (
	"bytes"
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

// These options are explicit RPC choices, not new inherited URL defaults:
// includeObservations adds metadata, descending reverses native ordering, and
// resourcesOnly omits server storage rows. False and true must both survive an
// exact machine-context round trip independently of UI-link limitations.
func TestAddedReadBooleanDefaultsAndRoundTrips(t *testing.T) {
	for _, tc := range []struct{ method, field, path, base string }{
		{"GetTimeSeries", "includeObservations", "/servers/nats-0", `{"metric":"server.cpu_percent","entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0"}}`},
		{"ListMetricContributors", "descending", "/servers", `{"metric":"server.cpu_percent","ranking":"METRIC_RANKING_MAXIMUM"}`},
		{"GetStorageChange", "resourcesOnly", "/capacity/storage-change", `{}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			c, err := ContextFromURL("https://lens.example.com"+tc.path+"?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z", "https://lens.example.com", fixtureNow())
			if err != nil {
				t.Fatal(err)
			}
			field := Method(tc.method).Input().Fields().ByJSONName(tc.field)
			if field == nil {
				t.Fatal("audited field disappeared")
			}
			check := func(data []byte, want bool) {
				t.Helper()
				m := dynamicpb.NewMessage(Method(tc.method).Input())
				if err := protojson.Unmarshal(data, m); err != nil {
					t.Fatal(err)
				}
				if m.Get(field).Bool() != want {
					t.Fatalf("%s = %v in %s; want %v", tc.field, m.Get(field).Bool(), data, want)
				}
			}
			baseline, _, err := Prepare(tc.method, c, []byte(tc.base))
			if err != nil {
				t.Fatal(err)
			}
			check(baseline, false)
			for _, want := range []bool{false, true} {
				var values map[string]any
				if err := json.Unmarshal([]byte(tc.base), &values); err != nil {
					t.Fatal(err)
				}
				values[tc.field] = want
				override, _ := json.Marshal(values)
				request, _, err := Prepare(tc.method, c, override)
				if err != nil {
					t.Fatal(err)
				}
				check(request, want)
				effective, _, err := effectiveContext(tc.method, c, request)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(effective)
				reimported, err := ParseContext(bytes.NewReader(raw), c.Deployment)
				if err != nil {
					t.Fatal(err)
				}
				again, _, err := Prepare(tc.method, reimported, nil)
				if err != nil {
					t.Fatal(err)
				}
				check(again, want)
				a, b := dynamicpb.NewMessage(Method(tc.method).Input()), dynamicpb.NewMessage(Method(tc.method).Input())
				if err := protojson.Unmarshal(request, a); err != nil {
					t.Fatal(err)
				}
				if err := protojson.Unmarshal(again, b); err != nil {
					t.Fatal(err)
				}
				if !proto.Equal(a, b) {
					t.Fatalf("machine request changed: %s -> %s", request, again)
				}
			}
		})
	}
}
