package investigation

import (
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
	"strings"
	"testing"
)

func TestUIEntityAndEnumProof(t *testing.T) {
	for _, tc := range []struct {
		method, path, request string
		partial               bool
	}{
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_ACCOUNT","key":"APP","account":"APP"}}`, true},
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_CLIENT","key":"worker"}}`, true},
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_CONNECTION","key":"nats-0/900"}}`, true},
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0"}}`, false},
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_STREAM","key":"APP/orders"}}`, false},
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_CONSUMER","key":"APP/orders/worker"}}`, false},
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_SYSTEM"}}`, false},
		{"ListEvents", "/events?show=all", `{"entity":{"type":"ENTITY_TYPE_UNSPECIFIED"}}`, false},
		{"ListEvents", "/events?show=all", `{"entity":{"type":987,"key":"future"}}`, true},
		{"ListEvents", "/events?show=all", `{"sources":[0]}`, true},
		{"ListEvents", "/events?show=all", `{"sources":[987]}`, true},
		{"ListFindings", "/findings", `{"severities":["SEVERITY_UNSPECIFIED"]}`, true},
		{"ListFindings", "/findings", `{"severities":[0]}`, true},
		{"ListFindings", "/findings", `{"severities":[987]}`, true},
		{"ListFindings", "/findings", `{"severities":["SEVERITY_WARNING","SEVERITY_INFO"]}`, false},
		{"ListFindings", "/findings", `{"severities":[]}`, false},
		{"ListFindings", "/findings", `{"states":[0]}`, true},
		{"ListFindings", "/findings", `{"states":[987]}`, true},
		{"ListFindings", "/findings", `{"states":["FINDING_STATE_FIRING","FINDING_STATE_CHANGED","FINDING_STATE_FLAPPING","FINDING_STATE_RESOLVED"]}`, false},
		{"ListFindings", "/findings", `{"states":[]}`, false},
		{"ListConsumers", "/consumers", `{"sort":987}`, true},
		{"GetSubjects", "/subjects", `{"filter":987}`, true},
		{"GetPlacement", "/placement", `{"filter":987}`, true},
	} {
		t.Run(tc.method+tc.request, func(t *testing.T) {
			c, err := ContextFromURL("https://lens.example.com"+tc.path+"&system=shop", "https://lens.example.com", fixtureNow())
			if !strings.Contains(tc.path, "?") {
				c, err = ContextFromURL("https://lens.example.com"+tc.path+"?system=shop", "https://lens.example.com", fixtureNow())
			}
			if err != nil {
				t.Fatal(err)
			}
			req, _, err := Prepare(tc.method, c, []byte(tc.request))
			if err != nil {
				t.Fatal(err)
			}
			effective, limits, err := effectiveContext(tc.method, c, req)
			if err != nil {
				t.Fatal(err)
			}
			if (len(limits) > 0) != tc.partial {
				t.Errorf("partial=%v want %v: %v", len(limits) > 0, tc.partial, limits)
			}
			raw, _ := json.Marshal(effective)
			parsed, err := ParseContext(strings.NewReader(string(raw)), c.Deployment)
			if err != nil {
				t.Fatal(err)
			}
			again, _, err := Prepare(tc.method, parsed, nil)
			if err != nil {
				t.Fatal(err)
			}
			a := dynamicpb.NewMessage(Method(tc.method).Input())
			b := dynamicpb.NewMessage(Method(tc.method).Input())
			protojson.Unmarshal(req, a)
			protojson.Unmarshal(again, b)
			if !proto.Equal(a, b) {
				t.Fatalf("explicit request changed: %s / %s", req, again)
			}
		})
	}
}

func TestUnreviewedDefaultIsPartial(t *testing.T) {
	msg := dynamicpb.NewMessage(Method("ListConnections").Input())
	policies := map[string]string{}
	fields := msg.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		policies[string(fields.Get(i).Name())] = "predicate"
	}
	delete(policies, "include_tooling")
	limits := compareUIFields("ListConnections", msg.ProtoReflect(), msg.ProtoReflect(), policies)
	if len(limits) != 1 || !strings.Contains(limits[0], "includeTooling") {
		t.Fatal(limits)
	}
}

func TestNoUIProjectionCannotProveEmptyRequestScope(t *testing.T) {
	for _, method := range []string{"ListMetrics", "GetTimeSeriesBatch"} {
		c, _ := ContextFromURL("https://lens.example.com/?system=shop", "https://lens.example.com", fixtureNow())
		req, _, err := Prepare(method, c, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, limits, err := effectiveContext(method, c, req)
		if err != nil {
			t.Fatal(err)
		}
		if len(limits) == 0 {
			t.Errorf("%s has no dedicated exact UI projection", method)
		}
	}
}
