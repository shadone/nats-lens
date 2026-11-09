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
