package investigation

import (
	"encoding/json"
	"fmt"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
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
		{"ListEvents", "/events?show=all", `{"selectedIds":[]}`, false},
		{"ListEvents", "/events?show=all", `{"selectedIds":["1"]}`, true},
		{"ListEvents", "/events?show=all", `{"eventReadId":"0"}`, true},
		{"GetEvent", "/events?id=1", `{"id":"1"}`, false},
		{"GetEvent", "/events?id=1", `{"id":"1","eventReadId":"0"}`, true},
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

func TestTypedKeyOwnsResourceProjectionAndLinks(t *testing.T) {
	for _, tc := range []struct{ kind, key, path, method, field, value string }{
		{"ENTITY_TYPE_SERVER", "nats-0", "/servers/nats-0", "GetServer", "name", "nats-0"},
		{"ENTITY_TYPE_STREAM", "APP/orders", "/streams/APP/orders", "GetStream", "stream", "orders"},
		{"ENTITY_TYPE_CONSUMER", "APP/orders/worker", "/consumers/APP/orders/worker", "GetConsumer", "consumer", "worker"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c, _ := ContextFromURL("https://lens.example.com/events?system=shop&show=all", "https://lens.example.com", fixtureNow())
			raw, _ := json.Marshal(map[string]any{"entity": map[string]string{"type": tc.kind, "key": tc.key}})
			req, _, _ := Prepare("ListEvents", c, raw)
			effective, limits, err := effectiveContext("ListEvents", c, req)
			if err != nil {
				t.Fatal(err)
			}
			if len(limits) > 0 {
				t.Fatal(limits)
			}
			source, _ := effective.Link(c.Deployment)
			links := evidenceLinks("ListEvents", []byte(`{}`), effective, c.Deployment, source)
			found := false
			for _, l := range links {
				if l.Kind == "selected_resource" && l.ID == tc.key && strings.Contains(l.URL, tc.path+"?") {
					found = true
				}
			}
			if !found {
				t.Errorf("typed-key resource link missing: %v", links)
			}
			next, _, err := Prepare(tc.method, effective, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]json.RawMessage
			json.Unmarshal(next, &got)
			var value string
			json.Unmarshal(got[tc.field], &value)
			if value != tc.value {
				t.Errorf("typed-key followup lost identity: %s", next)
			}
		})
	}
}

func TestFindingEvidenceProvesOnCallProjection(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/findings/consumer-001%3AAPP%2Forders%2Fworker?system=shop", "https://lens.example.com", fixtureNow())
	req, _, err := Prepare("GetOnCall", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		FindingID string `json:"findingId"`
	}
	json.Unmarshal(req, &got)
	if got.FindingID != "consumer-001:APP/orders/worker" {
		t.Fatalf("finding route identity missing: %s", req)
	}
	_, limits, err := effectiveContext("GetOnCall", c, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 0 {
		t.Fatal(limits)
	}
}

func TestPredicateProofDistinguishesOneofZeroFromAbsent(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{Name: proto.String("fixture.proto"), Package: proto.String("fixture"), Syntax: proto.String("proto3"), MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Selection"), OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("identity")}}, Field: []*descriptorpb.FieldDescriptorProto{{Name: proto.String("selector"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), OneofIndex: proto.Int32(0)}}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := file.Messages().Get(0)
	f := md.Fields().Get(0)
	request := dynamicpb.NewMessage(md)
	ui := dynamicpb.NewMessage(md)
	request.Set(f, protoreflect.ValueOfString(""))
	policies := map[string]string{"selector": "predicate"}
	if len(compareUIFields("fixture", request, ui, policies)) == 0 {
		t.Fatal("present oneof empty value equals absent")
	}
	ui.Set(f, protoreflect.ValueOfString(""))
	if limits := compareUIFields("fixture", request, ui, policies); len(limits) != 0 {
		t.Fatal(limits)
	}
}

func TestTimelineRowCoverageOptionIsNotAnEncodedUIPredicate(t *testing.T) {
	c, err := ContextFromURL("https://lens.example.com/?system=shop&range=1h", "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []bool{false, true} {
		raw, _, err := Prepare("GetStatusTimeline", c, []byte(fmt.Sprintf(`{"includeOk":%t}`, value)))
		if err != nil {
			t.Fatal(err)
		}
		var req v1.GetStatusTimelineRequest
		if err := protojson.Unmarshal(raw, &req); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(proveUILinkScope("GetStatusTimeline", req.ProtoReflect(), c), " "), "includeOk") {
			t.Fatalf("includeOk=%v silently treated as page-equivalent", value)
		}
	}
}
