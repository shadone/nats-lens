package investigation

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestEffectiveRequestReplacesStaleUIScope(t *testing.T) {
	original, _ := ContextFromURL("https://lens.example.com/events?system=old&account=OTHER&server=old&kind=old&client=old&src=nats&show=worth&id=old", "https://lens.example.com", fixtureNow())
	before, _ := json.Marshal(original)
	for _, test := range []struct {
		method, request, path string
		query                 map[string]string
		absent                []string
	}{
		{"ListStreams", `{"system":"shop","account":"APP","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"}}`, "/streams", map[string]string{"system": "shop", "account": "APP", "from": "2026-10-17T13:00:00Z"}, []string{"server", "kind", "client", "src", "id", "show"}},
		{"ListConsumers", `{"system":"shop","account":"APP","stream":"orders","sort":"CONSUMER_SORT_DELIVERED_RATE"}`, "/consumers", map[string]string{"account": "APP", "stream": "APP/orders"}, []string{"server", "id"}},
		{"ListEvents", `{"system":"shop","entity":{"type":"ENTITY_TYPE_CONSUMER","key":"APP/orders/worker","account":"APP","stream":"orders","consumer":"worker"},"kinds":["finding.resolved"],"clientName":"checkout","sources":["EVENT_SOURCE_FINDING"],"hideRoutine":false}`, "/events", map[string]string{"consumer": "APP/orders/worker", "kind": "finding.resolved", "client": "checkout", "src": "findings", "show": "all"}, []string{"server", "id"}},
		{"ListFindings", `{"system":"shop","checkCode":"consumer-001","states":["FINDING_STATE_RESOLVED"],"includeMuted":false}`, "/findings", map[string]string{"check": "consumer-001", "state": "resolved", "muted": "0"}, []string{"server", "kind", "client", "src", "id"}},
		{"GetStream", `{"system":"shop","account":"APP","stream":"orders"}`, "/streams/APP/orders", map[string]string{"account": "APP"}, []string{"server", "id"}},
		{"GetConsumer", `{"system":"shop","account":"APP","stream":"orders","consumer":"worker"}`, "/consumers/APP/orders/worker", map[string]string{"account": "APP"}, []string{"server", "id"}},
		{"GetServer", `{"system":"shop","name":"nats-new"}`, "/servers/nats-new", map[string]string{}, []string{"account", "server", "id"}},
		{"GetFinding", `{"system":"shop","id":"consumer-001:APP/orders/worker"}`, "/findings/consumer-001:APP/orders/worker", map[string]string{}, []string{"account", "server", "id"}},
		{"GetEvent", `{"system":"shop","id":"new-event"}`, "/events", map[string]string{"id": "new-event"}, []string{"account", "server", "kind"}},
		{"GetReplay", `{"system":"shop","a":"2026-10-17T13:00:00Z","b":"2026-10-17T14:00:00Z"}`, "/replay", map[string]string{"a": "2026-10-17T13:00:00Z", "b": "2026-10-17T14:00:00Z", "from": "2026-10-17T13:00:00Z"}, []string{"server", "id"}},
		{"GetTimeSeries", `{"system":"shop","metric":"stream.bytes","entity":{"type":"ENTITY_TYPE_STREAM","key":"APP/orders","account":"APP","stream":"orders"}}`, "/streams/APP/orders", map[string]string{"account": "APP"}, []string{"server", "id"}},
	} {
		t.Run(test.method, func(t *testing.T) {
			effective, _, err := effectiveContext(test.method, original, []byte(test.request))
			if err != nil {
				t.Fatal(err)
			}
			link, err := effective.Link(original.Deployment)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(link)
			if u.Path != test.path {
				t.Errorf("path %s, want %s", u.Path, test.path)
			}
			for k, v := range test.query {
				if got := u.Query().Get(k); got != v {
					t.Errorf("%s=%q, want %q: %s", k, got, v, link)
				}
			}
			for _, k := range test.absent {
				if u.Query().Has(k) {
					t.Errorf("stale %s survived: %s", k, link)
				}
			}
			after, _ := json.Marshal(original)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("original context mutated")
			}
		})
	}
}
func TestCanonicalContextFieldsWinSourceQuery(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/streams?system=shop&account=OTHER", "https://lens.example.com", fixtureNow())
	c.Account = "APP"
	c.SourcePath += "?account=OTHER&system=old&range=1h"
	link, err := c.Link(c.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	if u.Query().Get("account") != "APP" || u.Query().Get("system") != "shop" || strings.Contains(link, "range=") {
		t.Fatal(link)
	}
}

func TestUnsupportedUIScopeRemainsExplicit(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/servers/old?system=shop&account=OTHER", "https://lens.example.com", fixtureNow())
	for _, test := range []struct{ method, request string }{{"GetDataHistory", `{"system":"shop","server":"new"}`}, {"GetTimeSeriesBatch", `{"system":"shop","queries":[{"metric":"cluster.connections"}]}`}, {"ListStreams", `{"system":"shop","summaryOnly":true}`}} {
		effective, limitations, err := effectiveContext(test.method, c, []byte(test.request))
		if err != nil {
			t.Fatal(err)
		}
		if len(limitations) == 0 {
			t.Errorf("%s falsely claims exact UI scope", test.method)
		}
		link, _ := effective.Link(c.Deployment)
		if strings.Contains(link, "old") || strings.Contains(link, "OTHER") {
			t.Fatal(link)
		}
	}
}

func TestSupportedURLFiltersRoundTripEffectiveRequest(t *testing.T) {
	for _, test := range []struct {
		method, path string
		want         map[string]string
	}{
		{"ListStreams", "/streams?system=shop&account=APP&kv=1", map[string]string{"kv": "1", "account": "APP"}},
		{"ListConsumers", "/consumers?system=shop&account=APP&stream=APP/orders&ephemeral=1", map[string]string{"ephemeral": "1", "stream": "APP/orders"}},
		{"ListConnections", "/connections?system=shop&server=new&account=APP&name=checkout&tooling=1", map[string]string{"server": "new", "account": "APP", "name": "checkout", "tooling": "1"}},
		{"GetSlowConsumerInvestigation", "/servers/slow-consumers?system=shop&server=new&account=APP&process=process-new&epoch=9007199254740993", map[string]string{"server": "new", "account": "APP", "process": "process-new", "epoch": "9007199254740993"}},
	} {
		t.Run(test.method, func(t *testing.T) {
			c, err := ContextFromURL("https://lens.example.com"+test.path, "https://lens.example.com", fixtureNow())
			if err != nil {
				t.Fatal(err)
			}
			req, unapplied, err := Prepare(test.method, c, nil)
			if err != nil || len(unapplied) > 0 {
				t.Fatalf("%s %v %v", req, unapplied, err)
			}
			effective, _, err := effectiveContext(test.method, c, req)
			if err != nil {
				t.Fatal(err)
			}
			link, err := effective.Link(c.Deployment)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(link)
			for k, v := range test.want {
				if u.Query().Get(k) != v {
					t.Fatalf("lost %s: %s", k, link)
				}
			}
		})
	}
}
