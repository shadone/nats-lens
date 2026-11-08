package investigation

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Expected values were audited against each page's query construction. These
// fixtures distinguish UI row predicates from metadata, local state and budgets.
func TestSupportedPageEffectiveFilterContracts(t *testing.T) {
	for _, tc := range []struct {
		page, method, defaults, override string
		partial                          bool
		unapplied                        string
	}{
		{"/?account=APP", "GetOverview", `{}`, `{}`, false, "account"},
		{"/fleet", "ListSystems", `{}`, `{}`, false, ""},
		{"/servers", "ListServers", `{}`, `{}`, false, ""},
		{"/servers/nats-0?account=APP", "GetServer", `{"name":"nats-0"}`, `{"name":"nats-1"}`, false, "account"},
		{"/streams?kv=0", "ListStreams", `{"includeKv":false}`, `{"includeKv":true}`, false, ""},
		{"/streams?kv=true", "ListStreams", `{"includeKv":false}`, `{"summaryOnly":true}`, true, ""},
		{"/streams/APP/orders?account=OTHER", "GetStream", `{"account":"APP","stream":"orders"}`, `{"account":"NEW"}`, false, ""},
		{"/consumers?account=OTHER&stream=APP/orders&ephemeral=0", "ListConsumers", `{"account":"APP","stream":"orders","includeEphemeral":false}`, `{"includeEphemeral":true}`, false, ""},
		{"/consumers?ephemeral=true", "ListConsumers", `{"includeEphemeral":false}`, `{"sort":"CONSUMER_SORT_DELIVERED_RATE"}`, true, ""},
		{"/consumers/APP/orders/worker", "GetConsumer", `{"consumer":"worker","account":"APP","stream":"orders"}`, `{"consumer":"other"}`, false, ""},
		{"/kv-stores/APP/catalog", "GetStream", `{"account":"APP","stream":"KV_catalog"}`, `{"stream":"KV_other"}`, false, ""},
		{"/kv-stores", "ListKeyValueStores", `{}`, `{}`, false, ""},
		{"/object-stores/APP/images", "GetStream", `{"stream":"OBJ_images"}`, `{"stream":"OBJ_other"}`, false, ""},
		{"/object-stores", "ListObjectStores", `{}`, `{}`, false, ""},
		{"/findings", "ListFindings", `{"includeMuted":true,"states":["FINDING_STATE_FIRING","FINDING_STATE_CHANGED","FINDING_STATE_FLAPPING"]}`, `{"includeMuted":false}`, false, ""},
		{"/findings?muted=0&state=resolved", "ListFindings", `{"includeMuted":false,"states":["FINDING_STATE_RESOLVED"]}`, `{"states":["FINDING_STATE_FIRING"]}`, true, ""},
		{"/findings/fixture", "GetFinding", `{"id":"fixture"}`, `{"id":"other"}`, false, ""},
		{"/checks", "ListCheckDefinitions", `{}`, `{}`, false, ""},
		{"/events", "ListEvents", `{"hideRoutine":true,"foldRepeats":true}`, `{"hideRoutine":false}`, true, ""},
		{"/events?show=all", "ListEvents", `{"hideRoutine":false,"foldRepeats":false}`, `{"hideRoutine":true,"foldRepeats":true}`, false, ""},
		{"/events?id=one", "GetEvent", `{"id":"one"}`, `{"id":"two"}`, false, "show"},
		{"/connections?name=worker&tooling=0", "ListConnections", `{"includeTooling":true,"nameContains":"worker"}`, `{"includeTooling":false}`, true, ""},
		{"/applications?account=APP&tooling=0&sort=bytes&dir=asc", "GetApplications", `{"account":"APP","includeTooling":false}`, `{"includeTooling":true}`, false, "sort,dir"},
		{"/subjects?account=APP&filter=wide", "GetSubjects", `{"account":"APP","filter":"SUBJECT_FILTER_UNSPECIFIED"}`, `{"filter":"SUBJECT_FILTER_WIDE"}`, true, "filter"},
		{"/subjects?subject=orders.created", "MatchSubject", `{"subject":"orders.created"}`, `{"subject":"orders.updated"}`, true, ""},
		{"/micro-services?account=APP", "GetMicroServices", `{"account":"APP"}`, `{"account":"NEW"}`, false, ""},
		{"/capacity?account=APP", "GetCapacity", `{}`, `{}`, false, "account"},
		{"/placement?filter=missing&cmp=-1h&servers=all", "GetPlacement", `{"filter":"PLACEMENT_FILTER_UNSPECIFIED"}`, `{"filter":"PLACEMENT_FILTER_MISSING"}`, true, "filter,cmp,servers"},
		{"/topology?account=APP", "GetTopology", `{}`, `{}`, false, "account"},
		{"/accounts?account=APP", "GetAccountsAndAuth", `{}`, `{}`, false, "account"},
		{"/data-history", "GetDataHistory", `{}`, `{"server":"nats-0"}`, true, ""},
		{"/replay?a=2026-10-17T13:00:00Z&b=2026-10-17T14:00:00Z", "GetReplay", `{"a":"2026-10-17T13:00:00Z","b":"2026-10-17T14:00:00Z"}`, `{"a":"2026-10-17T13:30:00Z"}`, false, ""},
		{"/servers/slow-consumers?server=nats-0&account=APP&process=p&epoch=9007199254740993", "GetSlowConsumerInvestigation", `{"server":"nats-0","account":"APP","observationServerId":"p","observationEpoch":"9007199254740993"}`, `{"account":"NEW"}`, false, ""},
	} {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			sep := "?"
			if strings.Contains(tc.page, "?") {
				sep = "&"
			}
			c, err := ContextFromURL("https://lens.example.com"+tc.page+sep+"system=shop", "https://lens.example.com", fixtureNow())
			if err != nil {
				t.Fatal(err)
			}
			req, un, err := Prepare(tc.method, c, nil)
			if err != nil {
				t.Fatal(err)
			}
			msg := dynamicpb.NewMessage(Method(tc.method).Input())
			if err = protojson.Unmarshal(req, msg); err != nil {
				t.Fatal(err)
			}
			all, _ := protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(msg)
			var got, want map[string]any
			json.Unmarshal(all, &got)
			json.Unmarshal([]byte(tc.defaults), &want)
			for k, v := range want {
				a, _ := json.Marshal(got[k])
				b, _ := json.Marshal(v)
				if string(a) != string(b) {
					t.Errorf("%s: got %s want %s", k, a, b)
				}
			}
			for _, k := range strings.Split(tc.unapplied, ",") {
				if k != "" && !slices.Contains(un, k) {
					t.Errorf("missing unapplied filter %s: %v", k, un)
				}
			}
			req, _, err = Prepare(tc.method, c, []byte(tc.override))
			if err != nil {
				t.Fatal(err)
			}
			msg.Reset()
			if err = protojson.Unmarshal(req, msg); err != nil {
				t.Fatal(err)
			}
			all, _ = protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(msg)
			json.Unmarshal(all, &got)
			want = nil
			json.Unmarshal([]byte(tc.override), &want)
			for k, v := range want {
				a, _ := json.Marshal(got[k])
				b, _ := json.Marshal(v)
				if string(a) != string(b) {
					t.Errorf("explicit override lost %s: %s != %s", k, a, b)
				}
			}
			effective, limits, err := effectiveContext(tc.method, c, req)
			if err != nil {
				t.Fatal(err)
			}
			if (len(limits) > 0) != tc.partial {
				t.Errorf("unexpected UI equivalence: %v", limits)
			}
			link, err := effective.Link(c.Deployment)
			if err != nil || link == "" {
				t.Fatal(err)
			}
		})
	}
}

func TestEveryReadBooleanHasAuditedPolicy(t *testing.T) {
	audited := map[string]string{"ListFindings": "includeMuted", "ListEvents": "hideRoutine foldRepeats includeFacets", "GetStatusTimeline": "includeOk", "ListConnections": "includeTooling", "ListStreams": "includeKv summaryOnly", "ListConsumers": "includeEphemeral", "GetApplications": "includeTooling"}
	for _, method := range Methods() {
		fields := Method(method).Input().Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if f.Kind() == protoreflect.BoolKind && !slices.Contains(strings.Fields(audited[method]), f.JSONName()) {
				t.Errorf("audit new boolean policy %s.%s", method, f.JSONName())
			}
		}
	}
}

func TestChartFiltersDoNotBecomeSeriesPredicates(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/connections?system=shop&name=natslens&tooling=0", "https://lens.example.com", fixtureNow())
	c.Metric = "server.cpu_percent"
	c.Entity = &Entity{Type: "ENTITY_TYPE_SERVER", Key: "nats-0", Server: "nats-0"}
	req, un, err := Prepare("GetTimeSeries", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(un, "name") || !slices.Contains(un, "tooling") || strings.Contains(string(req), "includeTooling") {
		t.Fatalf("chart filters silently applied: %s %v", req, un)
	}
	effective, _, err := effectiveContext("GetTimeSeries", c, req)
	if err != nil {
		t.Fatal(err)
	}
	link, _ := effective.Link(c.Deployment)
	if strings.Contains(link, "name=") || strings.Contains(link, "tooling=") {
		t.Fatal(link)
	}
}

func TestUnsupportedUIAndAPIFiltersAreExplicit(t *testing.T) {
	for _, suffix := range []string{"includeInternal=1", "service=APP/orders", "tags=region:west", "order=asc", "cluster=west"} {
		if _, err := ContextFromURL("https://lens.example.com/micro-services?system=shop&"+suffix, "https://lens.example.com", fixtureNow()); err == nil {
			t.Fatalf("unimplemented URL filter accepted: %s", suffix)
		}
	}
	c, _ := ContextFromURL("https://lens.example.com/connections?system=shop", "https://lens.example.com", fixtureNow())
	if _, _, err := Prepare("ListConnections", c, []byte(`{"includeInternal":true}`)); err == nil {
		t.Fatal("invented includeInternal RPC field accepted")
	}
}
