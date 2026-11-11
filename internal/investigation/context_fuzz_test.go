package investigation

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const fuzzTrusted = "https://lens.example.com"

// fuzzURLs are page URLs the investigation tests read, valid and refused.
var fuzzURLs = []string{
	"https://lens.example.com/?system=shop&range=1h",
	"https://lens.example.com/?token=secret",
	"https://lens.example.com/capacity?system=a&system=b",
	"https://lens.example.com/capacity/storage-change?system=shop&account=APP&from=2026-10-17T13:00:00.000123Z&to=2026-10-17T14:00:00.000456Z&epochA=9007199254740993&epochB=9007199254740994",
	"https://lens.example.com/capacity/storage-change?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z&epochA=1&epochB=2&change=asc&resource=kv&contributors=1",
	"https://lens.example.com/connections?system=shop&name=natslens&tooling=0",
	"https://lens.example.com/consumers?system=shop&account=OTHER&stream=APP/orders",
	"https://lens.example.com/consumers/APP/orders/worker?system=shop&account=OTHER&stream=OLD/stale&consumer=OLD/stale/old&server=old",
	"https://lens.example.com/events?system=old&account=OTHER&server=old&kind=old&client=old&src=nats&show=worth&id=old",
	"https://lens.example.com/events?system=shop&kind=%20finding.resolved%20,,%20finding.firing%20&show=all",
	"https://lens.example.com/findings?system=shop",
	"https://lens.example.com/findings/consumer-001%3AAPP%2Forders%2Fworker?system=shop",
	"https://lens.example.com/findings/history?system=shop&from=2026-10-17T12:00:00.000123Z&to=2026-10-17T13:00:00.000456Z&readEpoch=0&check=stream-001&type=stream&key=APP%2Forders",
	"https://lens.example.com/findings/stream-001%3AAPP%2Forders/occurrences/9007199254740993?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z&readEpoch=9007199254740995",
	"https://lens.example.com/metrics/consumer-pending?system=shop&stream=APP%2Forders&account=OTHER&range=1h",
	"https://lens.example.com/metrics/messages?system=shop&server=nats-east&range=1h&page=2&rank=1",
	"https://lens.example.com/servers/nats-0?system=shop&range=1h&account=APP",
	"https://lens.example.com/streams/APP/orders?system=shop&account=OTHER",
	"https://lens.example.com/kv-stores/APP/catalog?system=shop",
	"https://lens.example.com/timeline/windows/" + strings.Repeat("a", 64) + "?system=shop&eventReadId=7",
	"https://lens.example.com/subjects?system=shop&subject=",
	"https://lens.example.com/not-supported",
	"https://lens.example.com/streams/APP/%2e%2e?system=shop",
	"https://lens.example.com/streams/APP/or%00ders?system=shop",
	"https://lens.example.com/events?system=sh%7Fop",
	"https://lens.example.com/events?system=shop&kind=%C2%85finding.resolved",
	"https://lens.example.com//evil.example.com/streams",
	"https://lens.example.com/events?system=shop&from=0001-01-01T00:00:00Z&to=9999-12-31T23:59:59.999999999Z",
	"https://lens.example.com/events?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T13:00:00Z",
	"https://lens.example.com/events?system=shop&from=2026-10-17T13:00:00%2B02:00&to=2026-10-17T14:00:00Z",
	"https://user:pass@lens.example.com/events",
	"http://lens.example.com/events",
	"https://lens.example.com/timeline/events?from=2026-10-17T12%3A00%3A00Z&system=shop&to=2026-10-17T13%3A00%3A00Z#selection=eyJ2ZXJzaW9uIjoxLCJzeXN0ZW0iOiJzaG9wIiwiZnJvbSI6IjIwMjYtMTAtMTdUMTI6MDA6MDBaIiwidG8iOiIyMDI2LTEwLTE3VDEzOjAwOjAwWiIsImlkcyI6WyIxIiwiMiJdLCJsaW1pdHMiOltdfQ",
}

// isC0OrDEL is a control character context text refuses. C1 characters
// (U+0080 to U+009F) are data: a kind filter keeps U+0085 as the UI does
// (TestUIListNormalizationContract).
func isC0OrDEL(r rune) bool { return r < 0x20 || r == 0x7f }

// plainTexts are the context's short text fields, which reach a prompt, a
// log line or a request, but never the free-form annotation.
func plainTexts(c *Context) []string {
	s := []string{c.System, c.Account, c.ChartLabel, c.Metric, c.Provenance.Reason}
	for k, v := range c.Filters {
		s = append(s, k, v)
	}
	entities := []*Entity{c.Entity}
	for _, ref := range c.Series {
		s = append(s, ref.Label, ref.Metric, ref.UnavailableReason)
		entities = append(entities, ref.Entity)
	}
	for _, e := range entities {
		if e != nil {
			s = append(s, e.Type, e.Key, e.Account, e.Stream, e.Consumer, e.Server)
		}
	}
	for _, e := range c.Evidence {
		s = append(s, e.Kind, e.ID)
	}
	return s
}

// checkContext checks what a context accepted from untrusted input
// promises: it validates, its short texts are valid UTF-8 without a C0 control
// character or DEL, it encodes and reads back to the same encoding,
// its link stays on the trusted deployment, and preparing a request from it
// is deterministic and gives a JSON object or an error.
func checkContext(t *testing.T, c *Context, method string) {
	t.Helper()
	if err := c.Validate(fuzzTrusted); err != nil {
		t.Fatalf("accepted context does not validate: %v", err)
	}
	for _, s := range plainTexts(c) {
		if !utf8.ValidString(s) || strings.ContainsFunc(s, isC0OrDEL) {
			t.Fatalf("accepted context holds text %q", s)
		}
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseContext(bytes.NewReader(encoded), fuzzTrusted)
	if err != nil {
		t.Fatalf("context %s does not read back: %v", encoded, err)
	}
	if again, _ := json.Marshal(back); !bytes.Equal(again, encoded) {
		t.Fatalf("context encoding is not stable:\n%s\n%s", encoded, again)
	}
	link, err := c.Link(fuzzTrusted)
	if err != nil || !strings.HasPrefix(link, fuzzTrusted+"/") {
		t.Fatalf("link %q of a valid context: %v", link, err)
	}
	if c.Query != nil {
		method = c.Query.Method
	}
	req, unapplied, err := Prepare(method, c, nil)
	req2, unapplied2, err2 := Prepare(method, c, nil)
	if (err == nil) != (err2 == nil) || !bytes.Equal(req, req2) || !reflect.DeepEqual(unapplied, unapplied2) {
		t.Fatalf("Prepare(%s) is not deterministic: %s %v / %s %v", method, req, err, req2, err2)
	}
	if err == nil {
		var fields map[string]json.RawMessage
		if json.Unmarshal(req, &fields) != nil {
			t.Fatalf("Prepare(%s) gave %s, not a JSON object", method, req)
		}
	}
}

// FuzzParseContext reads investigation context, which is pasted from
// untrusted prompts and files: it is accepted with every promise of
// checkContext, or refused with an error and no context.
func FuzzParseContext(f *testing.F) {
	for i, raw := range fuzzURLs {
		c, err := ContextFromURL(raw, fuzzTrusted, fixtureNow())
		if err != nil {
			continue
		}
		b, _ := json.Marshal(c)
		f.Add(b, uint8(i))
	}
	for _, s := range []string{
		`{}`, `null`, `[]`, `{"version":1}{"version":1}`, `{"version":1,"extra":1}`,
		`{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"},"sourcePath":"/events","provenance":{"status":"unavailable","reason":"r"}}`,
		`{"version":1,"deployment":"https://lens.example.com/","system":"shop","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"},"sourcePath":"/events?a=1&a=2","provenance":{"status":"unavailable","reason":"r"}}`,
		`{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"},"sourcePath":"/events","point":"2026-10-17T14:00:00Z","provenance":{"status":"unavailable","reason":"r"}}`,
		`{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"},"sourcePath":"/events","query":{"method":"ListEvents","request":{"system":"shop","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"}}},"provenance":{"status":"unavailable","reason":"r"}}`,
		`{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"},"sourcePath":"/events","query":{"method":"GetReplay","request":{"system":"shop","a":"2026-10-17T13:00:00Z","b":"2026-10-17T14:00:00Z"}},"provenance":{"status":"unavailable","reason":"r"}}`,
		`{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"},"sourcePath":"/streams/APP/orders","entity":{"type":"ENTITY_TYPE_STREAM","key":"APP/orders","account":"APP","stream":"orders"},"series":[{"label":"x","metric":"stream.messages"}],"provenance":{"status":"unavailable","reason":"r"}}`,
		`{"version":1,"deployment":"https://lens.example.com","system":"sh\u0000op","range":{"start":"2026-10-17T13:00:00Z","end":"2026-10-17T14:00:00Z"},"sourcePath":"/events","provenance":{"status":"unavailable","reason":"r"}}`,
		`{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"0001-01-01T00:00:00Z","end":"9999-12-31T23:59:59Z"},"sourcePath":"/events","provenance":{"status":"unavailable","reason":"r"}}`,
	} {
		f.Add([]byte(s), uint8(0))
	}
	methods := Methods()
	f.Fuzz(func(t *testing.T, data []byte, pick uint8) {
		if len(data) > MaxInputBytes+16 {
			return
		}
		c, err := ParseContext(bytes.NewReader(data), fuzzTrusted)
		c2, err2 := ParseContext(bytes.NewReader(data), fuzzTrusted)
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(c, c2) {
			t.Fatalf("ParseContext is not deterministic for %q", data)
		}
		if err != nil {
			if c != nil {
				t.Fatalf("refused context returned %+v", c)
			}
			return
		}
		checkContext(t, c, methods[int(pick)%len(methods)])
	})
}

// FuzzContextFromURL builds context from a page URL: a URL off the trusted
// deployment or with an unsupported path or parameter is refused, and an
// accepted one gives context with every promise of checkContext.
func FuzzContextFromURL(f *testing.F) {
	for i, raw := range fuzzURLs {
		f.Add(raw, uint8(i))
	}
	methods := Methods()
	f.Fuzz(func(t *testing.T, raw string, pick uint8) {
		if len(raw) > 16<<10 {
			return
		}
		c, err := ContextFromURL(raw, fuzzTrusted, fixtureNow())
		c2, err2 := ContextFromURL(raw, fuzzTrusted, fixtureNow())
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(c, c2) {
			t.Fatalf("ContextFromURL is not deterministic for %q", raw)
		}
		if err != nil {
			if c != nil {
				t.Fatalf("refused URL returned %+v", c)
			}
			return
		}
		if c.Deployment != fuzzTrusted {
			t.Fatalf("URL %q gave deployment %q", raw, c.Deployment)
		}
		checkContext(t, c, methods[int(pick)%len(methods)])
	})
}
