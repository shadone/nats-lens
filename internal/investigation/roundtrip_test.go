package investigation

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestEveryReadEffectiveContextRoundTrip(t *testing.T) {
	requests := map[string]string{
		"GetServer": `{"name":"nats-new"}`, "GetStream": `{"account":"APP","stream":"orders"}`, "GetConsumer": `{"account":"APP","stream":"orders","consumer":"worker"}`,
		"GetFinding": `{"id":"consumer-001:APP/orders/worker"}`, "GetOnCall": `{"findingId":"consumer-001:APP/orders/worker"}`, "GetEvent": `{"id":"991"}`,
		"GetTimeSeries":      `{"metric":"server.cpu_percent","entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0","server":"nats-0"},"maxPoints":7}`,
		"GetTimeSeriesBatch": `{"queries":[{"metric":"cluster.in_msgs_rate"},{"metric":"cluster.out_msgs_rate"}],"maxPoints":7}`,
		"ListStreams":        `{"account":"APP","includeKv":true,"summaryOnly":true,"limit":1,"pageToken":"bounded-fixture"}`,
		"ListConsumers":      `{"account":"APP","stream":"orders","includeEphemeral":true,"sort":"CONSUMER_SORT_DELIVERED_RATE","limit":1,"pageToken":"bounded-fixture"}`,
		"ListEvents":         `{"kinds":[" finding.resolved ","a,b", ""],"sources":["EVENT_SOURCE_FINDING"],"hideRoutine":false,"foldRepeats":true,"includeFacets":true,"histogramBuckets":7,"clientName":" exact client ","pageSize":1,"pageToken":"bounded-fixture"}`,
		"ListFindings":       `{"states":["FINDING_STATE_FIRING"],"severities":["SEVERITY_WARNING"],"includeMuted":false,"pageSize":1,"pageToken":"bounded-fixture"}`,
		"GetSubjects":        `{"account":"APP","filter":"SUBJECT_FILTER_WIDE"}`, "MatchSubject": `{"account":"APP","subject":"orders.created"}`,
		"GetPlacement":      `{"filter":"PLACEMENT_FILTER_MISSING","selectedAsset":"APP/orders","selectedServer":"nats-0"}`,
		"GetStatusTimeline": `{"includeOk":true,"bucket":"60s"}`, "GetApplications": `{"includeTooling":true,"account":"APP"}`,
		"GetDataHistory": `{"server":"nats-0"}`, "GetReplay": `{"a":"2026-10-17T13:00:00Z","b":"2026-10-17T14:00:00Z"}`,
		"GetSlowConsumerInvestigation": `{"server":"nats-0","account":"APP","observationServerId":"p","observationEpoch":"9007199254740993","limit":1}`,
	}
	for _, method := range Methods() {
		t.Run(method, func(t *testing.T) {
			c, _ := ContextFromURL("https://lens.example.com/consumers/OLD/stale/old?system=shop&account=OTHER&stream=OLD/stale&kind=old", "https://lens.example.com", fixtureNow())
			req, _, err := Prepare(method, c, []byte(requests[method]))
			if err != nil {
				t.Fatal(err)
			}
			original := dynamicpb.NewMessage(Method(method).Input())
			if err = protojson.Unmarshal(req, original); err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 3; round++ {
				effective, _, err := effectiveContext(method, c, req)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(effective)
				c, err = ParseContext(strings.NewReader(string(b)), c.Deployment)
				if err != nil {
					t.Fatal(err)
				}
				req, _, err = Prepare(method, c, nil)
				if err != nil {
					t.Fatal(err)
				}
				actual := dynamicpb.NewMessage(Method(method).Input())
				protojson.Unmarshal(req, actual)
				if !proto.Equal(original, actual) {
					t.Fatalf("round %d effective request changed: before=%s after=%s", round, original, req)
				}
			}
		})
	}
}

func TestReturnedContextClearAndStaleFilterPrecedence(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/consumers?system=shop&account=OTHER&stream=APP/orders", "https://lens.example.com", fixtureNow())
	req, _, _ := Prepare("ListConsumers", c, []byte(`{"includeEphemeral":true,"limit":1,"pageToken":"bounded-fixture"}`))
	effective, _, err := effectiveContext("ListConsumers", c, req)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Entity == nil || effective.Entity.Key != "APP/orders" {
		t.Fatal("selected stream identity missing")
	}
	// Residual display state cannot change a previously emitted machine request.
	effective.Filters["account"] = "OLD"
	effective.Filters["stream"] = "OLD/stale"
	for _, override := range []string{`{"account":"","stream":"","pageToken":""}`, `{"stream":"other","pageToken":"next"}`} {
		raw, _ := json.Marshal(effective)
		parsed, err := ParseContext(strings.NewReader(string(raw)), c.Deployment)
		if err != nil {
			t.Fatal(err)
		}
		next, _, err := Prepare("ListConsumers", parsed, []byte(override))
		if err != nil {
			t.Fatal(err)
		}
		var actual, want map[string]any
		json.Unmarshal(next, &actual)
		json.Unmarshal([]byte(override), &want)
		for k, v := range want {
			if v == "" && actual[k] == nil {
				continue
			}
			if actual[k] != v {
				t.Fatalf("explicit override %s: %s", override, next)
			}
		}
		if !strings.Contains(string(next), `"limit":1`) {
			t.Fatal("one-page budget lost")
		}
	}
}

func TestBoundQueryIsReadOnlyValidatedData(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/connections?system=shop", "https://lens.example.com", fixtureNow())
	b, _ := json.Marshal(c)
	var document map[string]any
	json.Unmarshal(b, &document)
	document["query"] = map[string]any{"method": "ListConnections", "request": map[string]any{"system": "shop", "range": c.Range, "nameContains": "natslens", "includeTooling": false, "pageSize": 1}}
	raw, _ := json.Marshal(document)
	parsed, err := ParseContext(strings.NewReader(string(raw)), c.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	req, _, err := Prepare("ListConnections", parsed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(req), `"includeTooling":true`) {
		t.Fatal("exact explicit exclusion changed")
	}
	for _, bad := range []map[string]any{
		{"method": "ExecuteSQL", "request": map[string]any{}},
		{"method": "ListConnections", "request": map[string]any{}},
		{"method": "ListConnections", "request": map[string]any{"system": "shop", "range": Bounds{"2026-10-17T13:00:00Z", "2026-10-17T14:00:00Z"}}},
		{"method": "ListConnections", "request": map[string]any{"authorization": "secret"}},
		{"method": "ListConnections", "request": map[string]any{"range": nil}},
		{"method": "ListConnections", "request": map[string]any{"pageSize": 501}},
		{"method": "ListConnections", "request": map[string]any{"system": "other"}},
	} {
		document["query"] = bad
		raw, _ = json.Marshal(document)
		if _, err = ParseContext(strings.NewReader(string(raw)), c.Deployment); err == nil {
			t.Errorf("invalid bound read accepted: %v", bad)
		}
	}
}
