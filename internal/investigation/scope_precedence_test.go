package investigation

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestURLCompoundScopePrecedence(t *testing.T) {
	for _, path := range []string{
		"/consumers?account=OTHER&stream=APP/orders",
		"/events?account=OTHER&server=old&stream=APP/orders",
		"/events?account=OTHER&server=old&stream=OLD/stale&consumer=APP/orders/worker",
		"/streams/APP/orders?account=OTHER&stream=OLD/stale&consumer=OLD/stale/old",
		"/consumers/APP/orders/worker?account=OTHER&stream=OLD/stale",
	} {
		c, err := ContextFromURL("https://lens.example.com"+path+"&system=shop", "https://lens.example.com", fixtureNow())
		if err != nil {
			t.Fatal(err)
		}
		// Deterministic invariant: import resolves the compound identity before any map iteration.
		if c.Account != "APP" || c.Entity == nil || c.Entity.Account != "APP" {
			t.Errorf("%s: unresolved context scope: %+v", path, c)
		}
	}
}

func TestTypedScopeBeforeExplicitOverrides(t *testing.T) {
	for _, method := range []string{"ListConsumers", "GetStream", "GetConsumer", "ListStreams", "ListEvents", "ListFindings", "GetTimeSeries"} {
		t.Run(method, func(t *testing.T) {
			c, _ := ContextFromURL("https://lens.example.com/consumers/APP/orders/worker?system=shop&account=OTHER&stream=OLD/stale&consumer=OLD/stale/old&server=old", "https://lens.example.com", fixtureNow())
			c.Account = "OTHER" // Older copied contexts may retain page account beside a stronger selected resource.
			c.Metric = "consumer.delivered_rate"
			before, _ := json.Marshal(c)
			for i := 0; i < 100; i++ {
				req, _, err := Prepare(method, c, nil)
				if err != nil {
					t.Fatal(err)
				}
				var r map[string]json.RawMessage
				json.Unmarshal(req, &r)
				if _, has := r["account"]; has && string(r["account"]) != `"APP"` {
					t.Fatalf("%s", req)
				}
				if method == "ListConsumers" || method == "GetStream" || method == "GetConsumer" {
					if string(r["stream"]) != `"orders"` {
						t.Fatalf("missing selected parent stream: %s", req)
					}
				}
				effective, _, err := effectiveContext(method, c, req)
				if err != nil {
					t.Fatal(err)
				}
				link, err := effective.Link(c.Deployment)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(link, "OTHER") || strings.Contains(link, "OLD") || strings.Contains(link, "stale") {
					t.Fatal(link)
				}
			}
			req, _, err := Prepare(method, c, []byte(`{"account":"EXPLICIT"}`))
			if Method(method).Input().Fields().ByJSONName("account") != nil {
				if err != nil {
					t.Fatal(err)
				}
				var r map[string]json.RawMessage
				json.Unmarshal(req, &r)
				if string(r["account"]) != `"EXPLICIT"` {
					t.Fatal(string(req))
				}
				effective, _, err := effectiveContext(method, c, req)
				if err != nil {
					t.Fatal(err)
				}
				link, _ := effective.Link(c.Deployment)
				u, _ := url.Parse(link)
				if u.Query().Get("account") != "EXPLICIT" {
					t.Fatal(link)
				}
			}
			after, _ := json.Marshal(c)
			if string(before) != string(after) {
				t.Fatal("Prepare mutated imported context")
			}
		})
	}
}

func TestSelectedEvidenceOutranksRetainedFilter(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/events?system=shop&id=old", "https://lens.example.com", fixtureNow())
	c.Evidence = []Evidence{{Kind: "event", ID: "selected"}}
	req, _, err := Prepare("GetEvent", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]string
	json.Unmarshal(req, &r)
	if r["id"] != "selected" {
		t.Fatal(string(req))
	}
	req, _, err = Prepare("GetEvent", c, []byte(`{"id":"explicit"}`))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(req, &r)
	if r["id"] != "explicit" {
		t.Fatal(string(req))
	}
}

func TestPageSelectorsCannotInventStrongerScope(t *testing.T) {
	for _, tc := range []struct{ path, account, key string }{
		{"/consumers?account=OTHER&stream=APP/orders&consumer=OLD/stale/old&server=old", "APP", "APP/orders"},
		{"/connections?account=OTHER&server=new&stream=APP/orders&consumer=APP/orders/worker", "OTHER", "new"},
		{"/servers/slow-consumers?account=OTHER&server=new&stream=APP/orders", "OTHER", "new"},
	} {
		c, err := ContextFromURL("https://lens.example.com"+tc.path+"&system=shop", "https://lens.example.com", fixtureNow())
		if err != nil {
			t.Fatal(err)
		}
		if c.Account != tc.account || c.Entity == nil || c.Entity.Key != tc.key {
			t.Fatalf("%s: %+v", tc.path, c)
		}
	}
}
