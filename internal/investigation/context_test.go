package investigation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestContextTrustAndBounds(t *testing.T) {
	good := `{"version":1,"deployment":"https://lens.example.com","system":"shop","account":"APP","range":{"start":"2026-10-17T13:30:00Z","end":"2026-10-17T14:30:00Z"},"sourcePath":"/capacity","provenance":{"status":"unavailable","reason":"API does not pin a collection"}}`
	c, err := ParseContext(strings.NewReader(good), "https://lens.example.com")
	if err != nil || c.System != "shop" {
		t.Fatalf("%+v %v", c, err)
	}
	for _, bad := range []string{
		strings.Replace(good, `"version":1`, `"version":2`, 1),
		strings.Replace(good, "https://lens.example.com", "https://evil.example.com", 1),
		strings.Replace(good, `"sourcePath":"/capacity"`, `"sourcePath":"//evil.example.com/"`, 1),
		strings.Replace(good, "2026-10-17T14:30:00Z", "2026-10-17T12:30:00Z", 1),
		strings.Replace(good, "2026-10-17T13:30:00Z", "2025-10-17T13:30:00Z", 1),
		strings.Replace(good, `"sourcePath":"/capacity"`, `"sourcePath":"/capacity?token=secret"`, 1),
		strings.Replace(good, `"system":"shop"`, `"system":"`+strings.Repeat("x", 257)+`"`, 1),
		good + `{}`, strings.Repeat("x", 65537),
	} {
		if _, err := ParseContext(strings.NewReader(bad), "https://lens.example.com"); err == nil {
			t.Errorf("accepted invalid context: %.150s", bad)
		}
	}
}

func TestPageURLFreezesServerClock(t *testing.T) {
	now := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	c, err := ContextFromURL("https://lens.example.com/servers/nats-0?system=shop&range=1h&account=APP", "https://lens.example.com", now)
	if err != nil || c.Range.Start != "2026-10-17T13:30:00Z" || c.Entity == nil || c.Entity.Key != "nats-0" {
		t.Fatalf("%+v %v", c, err)
	}
	for _, u := range []string{"https://evil.example.com/capacity", "https://lens.example.com/capacity?token=secret", "https://lens.example.com/capacity?range=unknown", "https://lens.example.com/not-supported", "https://lens.example.com/capacity?system=a&system=b"} {
		if _, err := ContextFromURL(u, "https://lens.example.com", now); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	link, err := c.Link("https://lens.example.com")
	if err != nil || !strings.Contains(link, "system=shop") || !strings.Contains(link, "from=") || strings.Contains(link, "range=") {
		t.Fatalf("%s %v", link, err)
	}
}
func fixtureNow() time.Time { return time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC) }

func TestContextRoutesAndInvalidIdentity(t *testing.T) {
	for _, path := range []string{"/streams/APP/orders", "/kv-stores/APP/catalog", "/object-stores/APP/images", "/consumers/APP/orders/worker", "/events?id=event-1", "/findings/f-1"} {
		c, err := ContextFromURL("https://lens.example.com"+path, "https://lens.example.com", fixtureNow())
		if err != nil {
			t.Fatal(err)
		}
		if c.Entity == nil && len(c.Evidence) == 0 {
			t.Fatal("identity missing")
		}
		b, _ := json.Marshal(c)
		if _, err = ParseContext(strings.NewReader(string(b)), c.Deployment); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := ContextFromURL("https://lens.example.com/capacity", "https://lens.example.com", fixtureNow())
	cases := []func(*Context){
		func(c *Context) { c.Metric = "not a metric" }, func(c *Context) { c.Point = "2020-01-01T00:00:00Z" },
		func(c *Context) { c.Filters = map[string]string{"token": "secret"} }, func(c *Context) { c.Entity = &Entity{Type: "unknown"} },
		func(c *Context) { c.Entity = &Entity{Type: "ENTITY_TYPE_SERVER", Key: strings.Repeat("x", 257)} },
		func(c *Context) { c.Evidence = []Evidence{{"unknown", "id"}} }, func(c *Context) { c.Provenance.Status = "pinned" },
		func(c *Context) { c.Range.Start = "2026-10-17T13:30:00+00:00" }, func(c *Context) { c.SourcePath = "/capacity?system=a&system=b" },
	}
	for _, change := range cases {
		copy := *c
		change(&copy)
		if err := copy.Validate(c.Deployment); err == nil {
			t.Fatalf("accepted %+v", copy)
		}
	}
}

// A Timeline client bar's bound ListEvents query, as the UI copies it,
// parses and prepares.
func TestTimelineClientCellBoundQuery(t *testing.T) {
	raw := `{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"2026-10-17T09:15:00.000Z","end":"2026-10-17T09:20:00.000Z"},"sourcePath":"/","filters":{},
"query":{"method":"ListEvents","request":{"range":{"start":"2026-10-17T09:15:00.000Z","end":"2026-10-17T09:20:00.000Z"},"kinds":["client.connect","client.disconnect","client.churn","client.auth_error"],"pageSize":50,"eventReadId":"99","system":"shop"}},
"provenance":{"status":"unavailable","reason":"x"}}`
	c, err := ParseContext(strings.NewReader(raw), "https://lens.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Prepare("ListEvents", c, nil); err != nil {
		t.Fatal(err)
	}
}
