package investigation

import (
	"encoding/json"
	"fmt"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"slices"
	"strings"
	"time"
)

// Every request field needs a reviewed policy. Unknown fields (even unset
// defaults) cannot establish UI equivalence. Budgets do not promise the same
// page cardinality; other unencoded options are partial when active.
var uiPredicates = map[string]string{
	"GetLensInfo": "system", "ListSystems": "range", "ListCheckDefinitions": "system",
	"GetOverview": "system range", "GetShellStatus": "system range",
	"GetTimeSeries": "system range entity", "GetTimeSeriesBatch": "system range",
	"ListFindings": "system range states severities include_muted check_code", "GetFinding": "system id", "GetOnCall": "system finding_id",
	"ListEvents": "system range entity sources kinds client_name hide_routine fold_repeats", "GetEvent": "system id",
	"GetStatusTimeline": "system range", "ListServers": "system range", "GetServer": "system range name", "ListAccounts": "system range",
	"ListConnections": "system range server account name_contains include_tooling",
	"ListStreams":     "system range include_kv", "GetStream": "system range account stream",
	"ListConsumers": "system range account stream include_ephemeral sort", "GetConsumer": "system range account stream consumer",
	"ListKeyValueStores": "system range account", "ListObjectStores": "system range account", "GetCapacity": "system range",
	"GetDataHistory": "system range", "GetPlacement": "system range filter", "GetReplay": "system a b", "GetTopology": "system range",
	"GetAccountsAndAuth": "system range", "GetSubjects": "system range account filter", "MatchSubject": "system range account",
	"GetMicroServices": "system range account", "GetApplications": "system range account include_tooling",
	"GetSlowConsumerInvestigation": "system range server account observation_server_id observation_epoch",
	"GetStorageChange":             "system account a b epoch_a epoch_b order resource_kind resources_only",
}
var uiDefaultOnly = map[string]string{
	"ListSystems":      "strip_buckets",
	"GetStorageChange": "page_token",
	"ListFindings":     "entity page_token text account", "ListEvents": "page_token include_facets histogram_buckets selected_ids event_read_id text",
	"GetEvent":          "event_read_id",
	"GetStatusTimeline": "entity event_read_id", "ListConnections": "page_token", "ListStreams": "account page_token summary_only",
	"ListConsumers": "page_token", "GetDataHistory": "server", "GetPlacement": "selected_asset selected_server",
}

func proveUILinkScope(method string, request protoreflect.Message, c *Context) []string {
	predicate, known := uiPredicates[method]
	if !known {
		return []string{"No reviewed UI request projection for this method"}
	}
	link, err := c.Link(c.Deployment)
	if err != nil {
		return []string{"UI projection could not be validated"}
	}
	end, _ := time.Parse(time.RFC3339Nano, c.Range.End)
	imported, err := ContextFromURL(link, c.Deployment, end)
	if err != nil {
		return []string{"UI link cannot be imported as a supported page"}
	}
	projected, _, err := Prepare(method, imported, nil)
	if err != nil {
		return []string{"UI page request projection is unavailable"}
	}
	ui := dynamicpb.NewMessage(request.Descriptor())
	if protojson.Unmarshal(projected, ui) != nil {
		return []string{"UI request projection does not match this RPC"}
	}
	policies := map[string]string{}
	for _, n := range strings.Fields(predicate) {
		policies[n] = "predicate"
	}
	for _, n := range strings.Fields(uiDefaultOnly[method]) {
		policies[n] = "default"
	}
	for _, n := range strings.Fields(map[string]string{"GetTimeSeries": "metric", "GetTimeSeriesBatch": "queries", "GetStatusTimeline": "bucket include_ok", "MatchSubject": "subject"}[method]) {
		policies[n] = "unencoded"
	}
	for _, n := range strings.Fields("page_size limit max_points") {
		if request.Descriptor().Fields().ByName(protoreflect.Name(n)) != nil {
			policies[n] = "budget"
		}
	}
	return compareUIFields(method, request, ui.ProtoReflect(), policies)
}
func compareUIFields(method string, request, ui protoreflect.Message, policies map[string]string) []string {
	limits := []string{}
	fields := request.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		policy := policies[string(f.Name())]
		switch policy {
		case "budget":
			continue
		case "unencoded":
			limits = append(limits, "UI request projection is unavailable for field "+f.JSONName())
			continue
		case "default":
			// Reviewed UI defaults express no restriction. Active options have
			// no encoded selector; unknown fields never enter this category.
			if request.Has(f) {
				limits = append(limits, "UI link cannot reproduce request option "+f.JSONName())
			}
			continue
		case "predicate":
			equal := false
			switch {
			case method == "ListEvents" && f.Name() == "entity":
				equal = eventEntityPredicate(request.Get(f).Message()) == eventEntityPredicate(ui.Get(f).Message())
			case f.IsList() && f.Kind() == protoreflect.EnumKind:
				equal = slices.Equal(enumPredicate(method, string(f.Name()), request.Get(f).List()), enumPredicate(method, string(f.Name()), ui.Get(f).List()))
			default:
				a := dynamicpb.NewMessage(request.Descriptor())
				b := dynamicpb.NewMessage(request.Descriptor())
				if request.Has(f) {
					a.Set(f, request.Get(f))
				}
				if ui.Has(f) {
					b.Set(f, ui.Get(f))
				}
				equal = proto.Equal(a, b)
			}
			if !equal {
				limits = append(limits, "UI link has no proven equivalent for request field "+f.JSONName())
			}
		default:
			limits = append(limits, "UI equivalence is unreviewed for request field "+f.JSONName())
		}
	}
	return limits
}
func enumPredicate(method, field string, list protoreflect.List) []int32 {
	values := []int32{}
	for i := 0; i < list.Len(); i++ {
		values = append(values, int32(list.Get(i).Enum()))
	}
	// The API's documented empty Findings state list means open, not all.
	if method == "ListFindings" && field == "states" && len(values) == 0 {
		values = []int32{int32(v1.FindingState_FINDING_STATE_FIRING), int32(v1.FindingState_FINDING_STATE_CHANGED), int32(v1.FindingState_FINDING_STATE_FLAPPING)}
	}
	slices.Sort(values)
	return slices.Compact(values)
}
func eventEntityPredicate(e protoreflect.Message) string {
	if !e.IsValid() {
		return "all"
	}
	kind := e.Get(e.Descriptor().Fields().ByName("type")).Enum()
	key := e.Get(e.Descriptor().Fields().ByName("key")).String()
	if key == "" && (kind == protoreflect.EnumNumber(v1.EntityType_ENTITY_TYPE_SYSTEM) || kind == protoreflect.EnumNumber(v1.EntityType_ENTITY_TYPE_UNSPECIFIED)) {
		return "all"
	}
	return fmt.Sprintf("%d:%s", kind, key)
}

// Numeric future enums remain numeric in the bound machine request. Only the
// disposable UI decoding view uses decimal text so it can disclose a partial
// projection rather than rejecting or silently normalizing a valid enum.
func enumProjectionJSON(md protoreflect.MessageDescriptor, raw []byte) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	for i := 0; i < md.Fields().Len(); i++ {
		f := md.Fields().Get(i)
		value, ok := fields[f.JSONName()]
		if !ok || f.IsMap() {
			continue
		}
		convert := func(v json.RawMessage) json.RawMessage {
			if f.Kind() == protoreflect.EnumKind && len(v) > 0 && v[0] != '"' {
				b, _ := json.Marshal(string(v))
				return b
			}
			if f.Message() != nil {
				return enumProjectionJSON(f.Message(), v)
			}
			return v
		}
		if f.IsList() {
			var values []json.RawMessage
			_ = json.Unmarshal(value, &values)
			for j := range values {
				values[j] = convert(values[j])
			}
			fields[f.JSONName()], _ = json.Marshal(values)
		} else {
			fields[f.JSONName()] = convert(value)
		}
	}
	b, _ := json.Marshal(fields)
	return b
}
func knownContextEntity(e *Entity) bool {
	if e == nil {
		return true
	}
	return slices.Contains(strings.Fields("ENTITY_TYPE_SYSTEM ENTITY_TYPE_SERVER ENTITY_TYPE_ACCOUNT ENTITY_TYPE_STREAM ENTITY_TYPE_CONSUMER ENTITY_TYPE_CONNECTION ENTITY_TYPE_CLIENT"), e.Type)
}

// The API predicate identity is type/key. Redundant request display fields may
// be absent or inconsistent; derive UI resource components only from the
// protocol's canonical key. The exact original request stays in query.request.
func projectContextEntity(e *Entity) *Entity {
	if e == nil || !knownContextEntity(e) {
		return nil
	}
	out := &Entity{Type: e.Type, Key: e.Key}
	parts := strings.Split(e.Key, "/")
	switch e.Type {
	case "ENTITY_TYPE_SERVER":
		out.Server = e.Key
	case "ENTITY_TYPE_ACCOUNT":
		out.Account = e.Key
	case "ENTITY_TYPE_STREAM":
		if len(parts) == 2 {
			out.Account = parts[0]
			out.Stream = parts[1]
		}
	case "ENTITY_TYPE_CONSUMER":
		if len(parts) == 3 {
			out.Account = parts[0]
			out.Stream = parts[1]
			out.Consumer = parts[2]
		}
	case "ENTITY_TYPE_CONNECTION":
		if len(parts) == 2 {
			out.Server = parts[0]
		}
	}
	return out
}
