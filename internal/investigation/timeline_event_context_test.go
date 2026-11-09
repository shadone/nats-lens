package investigation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTimelineEventContextRoundTrip(t *testing.T) {
	for _, tc := range []struct{ path, method string }{
		{"/events?id=9007199254740993&eventReadId=9007199254740995", "GetEvent"},
		{"/timeline/windows/" + strings.Repeat("a", 64) + "?eventReadId=9007199254740995", "GetStatusTimeline"},
	} {
		c, err := ContextFromURL("https://lens.example.com"+tc.path+"&system=shop&from=2026-10-17T13:00:00.000123Z&to=2026-10-17T14:00:00.000456Z", "https://lens.example.com", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := Prepare(tc.method, c, nil)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if json.Unmarshal(data, &fields) != nil {
			t.Fatal("invalid JSON")
		}
		if fields["eventReadId"] != "9007199254740995" {
			t.Fatalf("lost boundary %s", data)
		}
		if tc.method == "GetEvent" && fields["id"] != "9007199254740993" {
			t.Fatalf("lost identity %s", data)
		}
		if c.Range.Start != "2026-10-17T13:00:00.000123Z" {
			t.Fatalf("lost precision %+v", c.Range)
		}
	}
}

func TestFindingHistoryURLPreservesCapturedScope(t *testing.T) {
	c, err := ContextFromURL("https://lens.example.com/findings/history?system=shop&from=2026-10-17T12:00:00.000123Z&to=2026-10-17T13:00:00.000456Z&readEpoch=0&check=stream-001&type=stream&key=APP%2Forders", "https://lens.example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Evidence) > 0 {
		t.Fatalf("history route invented finding identity: %+v", c.Evidence)
	}
	request, _, err := Prepare("ListFindingEpisodes", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if json.Unmarshal(request, &r) != nil {
		t.Fatal("invalid JSON")
	}
	if r["readEpoch"] != "0" || r["checkCode"] != "stream-001" {
		t.Fatalf("lost captured selection: %s", request)
	}
	entity := r["entity"].(map[string]any)
	if entity["type"] != "ENTITY_TYPE_STREAM" || entity["key"] != "APP/orders" {
		t.Fatalf("lost typed scope: %s", request)
	}
}

func TestFindingHistoryRequiresExplicitFixedBounds(t *testing.T) {
	const from = "2026-10-17T12:00:00.000123Z"
	const to = "2026-10-17T13:00:00.000456Z"
	for _, tc := range []struct {
		name, path, query string
		invalid           bool
	}{
		{"missing both", "/findings/history", "", true},
		{"captured but missing bounds", "/findings/history", "&readEpoch=5", true},
		{"relative only", "/findings/history", "&range=1h&readEpoch=5", true},
		{"encoded route relative only", "/findings/%68istory", "&range=24h&readEpoch=5", true},
		{"from only", "/findings/history", "&from=" + from, true},
		{"to only", "/findings/history", "&to=" + to, true},
		{"invalid endpoint", "/findings/history", "&from=not-a-time&to=" + to, true},
		{"empty endpoint", "/findings/history", "&from=&to=" + to, true},
		{"backwards", "/findings/history", "&from=" + to + "&to=" + from, true},
		{"equal", "/findings/history", "&from=" + from + "&to=" + from, true},
		{"explicit fixed", "/findings/history", "&from=" + from + "&to=" + to + "&readEpoch=0", false},
		{"fixed overrides relative navigation", "/findings/history", "&range=1h&from=" + from + "&to=" + to, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ContextFromURL("https://lens.example.com"+tc.path+"?system=shop"+tc.query, "https://lens.example.com", fixtureNow())
			if (err != nil) != tc.invalid {
				t.Fatalf("history fixed-bounds error=%v want invalid=%v", err, tc.invalid)
			}
			if !tc.invalid && (c.Range.Start != from || c.Range.End != to) {
				t.Fatalf("explicit bounds changed: %+v", c.Range)
			}
		})
	}
}
