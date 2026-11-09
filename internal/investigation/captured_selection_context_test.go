package investigation

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func capturedSelectionURL(t *testing.T) string {
	t.Helper()
	data := `{"version":1,"system":"shop","from":"2026-10-17T12:00:00.000123Z","to":"2026-10-17T13:00:00.000456Z","ids":["9007199254740993","9007199254740994"],"eventReadId":"9007199254740995","limits":["finding transitions"]}`
	q := url.Values{"system": {"shop"}, "from": {"2026-10-17T12:00:00.000123Z"}, "to": {"2026-10-17T13:00:00.000456Z"}, "eventReadId": {"9007199254740995"}, "offset": {"1"}, "pageSize": {"1"}}
	return "https://lens.example.com/timeline/events?" + q.Encode() + "#selection=" + base64.RawURLEncoding.EncodeToString([]byte(data))
}
func TestCapturedSelectionURLImportsExactPageAndRoundTrips(t *testing.T) {
	c, err := ContextFromURL(capturedSelectionURL(t), "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	if c.Query == nil || c.Query.Method != "ListEventSelection" || !strings.Contains(c.Annotation, "finding transitions") {
		t.Fatalf("lost captured selection: %+v", c)
	}
	request, _, err := Prepare("ListEventSelection", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(request, &fields)
	if fields["offset"] != float64(1) || fields["pageSize"] != float64(1) || fields["eventReadId"] != "9007199254740995" || len(fields["ids"].([]any)) != 2 {
		t.Fatalf("broadened page: %s", request)
	}
	encoded, _ := json.Marshal(c)
	restored, err := ParseContext(strings.NewReader(string(encoded)), "https://lens.example.com")
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := Prepare("ListEventSelection", restored, nil)
	if err != nil || string(again) != string(request) {
		t.Fatalf("roundtrip changed: %s %v", again, err)
	}
}
func TestCapturedSelectionURLRejectsChangedScope(t *testing.T) {
	for _, change := range []func(*url.URL){func(u *url.URL) { q := u.Query(); q.Set("system", "other"); u.RawQuery = q.Encode() }, func(u *url.URL) { u.Fragment = "" }, func(u *url.URL) { q := u.Query(); q.Del("from"); u.RawQuery = q.Encode() }, func(u *url.URL) { q := u.Query(); q.Set("offset", "2"); u.RawQuery = q.Encode() }, func(u *url.URL) { q := u.Query(); q.Set("pageSize", "51"); u.RawQuery = q.Encode() }} {
		u, _ := url.Parse(capturedSelectionURL(t))
		change(u)
		if _, err := ContextFromURL(u.String(), "https://lens.example.com", fixtureNow()); err == nil {
			t.Fatal("changed/missing captured scope accepted")
		}
	}
}
