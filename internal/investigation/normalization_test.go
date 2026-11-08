package investigation

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestUIListNormalizationContract(t *testing.T) {
	for _, tc := range []struct {
		method, page, key, raw, field string
		want                          []string
	}{
		{"ListEvents", "/events", "kind", " finding.resolved ,, finding.firing ", "kinds", []string{"finding.resolved", "finding.firing"}},
		{"ListEvents", "/events", "kind", "\uFEFF\u00a0finding.resolved\u2028,\u3000,", "kinds", []string{"finding.resolved"}},
		{"ListEvents", "/events", "kind", "\u0085finding.resolved\u0085", "kinds", []string{"\u0085finding.resolved\u0085"}},
		{"ListEvents", "/events", "src", "findings,,nats,unknown,nats", "sources", []string{"EVENT_SOURCE_NATS", "EVENT_SOURCE_FINDING"}},
		{"ListEvents", "/events", "src", " nats ,collector", "sources", []string{"EVENT_SOURCE_COLLECTOR"}},
		{"ListFindings", "/findings", "sev", "critical,,unknown,warning", "severities", []string{"SEVERITY_CRITICAL", "SEVERITY_WARNING"}},
		{"ListFindings", "/findings", "sev", " warning ,info", "severities", []string{"SEVERITY_INFO"}},
	} {
		c, err := ContextFromURL("https://lens.example.com"+tc.page+"?system=shop&"+tc.key+"="+url.QueryEscape(tc.raw), "https://lens.example.com", fixtureNow())
		if err != nil {
			t.Fatal(err)
		}
		req, _, err := Prepare(tc.method, c, nil)
		if err != nil {
			t.Errorf("%+v: %v", tc, err)
			continue
		}
		var got map[string]json.RawMessage
		json.Unmarshal(req, &got)
		var values []string
		json.Unmarshal(got[tc.field], &values)
		if !reflect.DeepEqual(values, tc.want) {
			t.Errorf("%s %q: %v != %v", tc.key, tc.raw, values, tc.want)
		}
	}
}

func TestExplicitKindsStayExactAndDiscloseNonUIValues(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/events?system=shop", "https://lens.example.com", fixtureNow())
	for _, kinds := range [][]string{{" finding.resolved "}, {""}, {"a,b"}, {"\uFEFFfinding.resolved"}, {"finding.resolved"}, {"\u0085finding.resolved\u0085"}} {
		override, _ := json.Marshal(map[string]any{"kinds": kinds})
		req, _, err := Prepare("ListEvents", c, override)
		if err != nil {
			t.Fatal(err)
		}
		var got struct{ Kinds []string }
		json.Unmarshal(req, &got)
		if !reflect.DeepEqual(kinds, got.Kinds) {
			t.Fatal("explicit kinds rewritten")
		}
		_, limits, err := effectiveContext("ListEvents", c, req)
		if err != nil {
			t.Fatal(err)
		}
		wantPartial := kinds[0] != "finding.resolved" && kinds[0] != "\u0085finding.resolved\u0085"
		if (len(limits) > 0) != wantPartial {
			t.Errorf("kinds %q: %v", kinds, limits)
		}
	}
}

func TestIdentityPayloadsAreNotTrimmed(t *testing.T) {
	c, err := ContextFromURL("https://lens.example.com/consumers?system=shop&stream="+url.QueryEscape(" APP / orders "), "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	req, _, err := Prepare("ListConsumers", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]string
	json.Unmarshal(req, &r)
	if r["account"] != " APP " || r["stream"] != " orders " {
		t.Fatal(string(req))
	}
}
