package investigation

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPortableMultiSeriesContextToBatch(t *testing.T) {
	raw := `{"version":1,"deployment":"https://lens.example.com","system":"shop","range":{"start":"2026-10-17T13:30:00Z","end":"2026-10-17T14:30:00Z"},"sourcePath":"/","chartLabel":"Traffic","series":[{"label":"in","metric":"cluster.in_msgs_rate"},{"label":"out","metric":"cluster.out_msgs_rate"}],"provenance":{"status":"unavailable","reason":"No universal query pin"}}`
	c, err := ParseContext(strings.NewReader(raw), "https://lens.example.com")
	if err != nil {
		t.Fatal(err)
	}
	req, unapplied, err := Prepare("GetTimeSeriesBatch", c, nil)
	if err != nil || len(unapplied) > 0 {
		t.Fatalf("%s %v %v", req, unapplied, err)
	}
	var r struct {
		Queries []struct {
			Metric string `json:"metric"`
		} `json:"queries"`
	}
	json.Unmarshal(req, &r)
	if len(r.Queries) != 2 || r.Queries[0].Metric != "cluster.in_msgs_rate" || r.Queries[1].Metric != "cluster.out_msgs_rate" {
		t.Fatal(string(req))
	}
	effective, _, err := effectiveContext("GetTimeSeriesBatch", c, req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(effective)
	if !strings.Contains(string(b), `"label":"out"`) || !strings.Contains(string(b), `"chartLabel":"Traffic"`) {
		t.Fatalf("labels lost: %s", b)
	}
	for _, bad := range []string{strings.Replace(raw, "cluster.out_msgs_rate", "not an ID", 1), strings.Replace(raw, `"metric":"cluster.out_msgs_rate"`, `"unavailableReason":""`, 1)} {
		if _, err := ParseContext(strings.NewReader(bad), c.Deployment); err == nil {
			t.Fatal("invalid series context accepted")
		}
	}
}

func TestResourceIdentityOutranksRetainedPageAccount(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/streams/APP/orders?system=shop&account=OTHER", "https://lens.example.com", fixtureNow())
	req, _, err := Prepare("GetStream", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]json.RawMessage
	json.Unmarshal(req, &r)
	if string(r["account"]) != `"APP"` {
		t.Fatalf("retained page account replaced selected resource identity: %s", req)
	}
}
