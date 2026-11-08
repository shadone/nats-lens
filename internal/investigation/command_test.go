package investigation

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCommandDiscovery(t *testing.T) {
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"methods"}, strings.NewReader(""), &out, &out); err != nil || !strings.Contains(out.String(), "GetTimeSeries") || strings.Contains(out.String(), "MuteFinding") {
		t.Fatalf("%s %v", out.String(), err)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"describe", "GetTimeSeries"}, strings.NewReader(""), &out, &out); err != nil || !strings.Contains(out.String(), "maxPoints") || !strings.Contains(out.String(), "ENTITY_TYPE_SERVER") {
		t.Fatalf("%s %v", out.String(), err)
	}
}
func TestPrepareRequestAndNarrowing(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/servers/nats-0?system=shop&range=1h", "https://lens.example.com", fixtureNow())
	c.Metric = "server.cpu"
	req, unapplied, err := Prepare("GetTimeSeries", c, []byte(`{"maxPoints":30}`))
	if err != nil || len(unapplied) != 0 || !strings.Contains(string(req), `"server.cpu"`) || !strings.Contains(string(req), "nats-0") {
		t.Fatalf("%s %v %v", req, unapplied, err)
	}
	if _, _, err = Prepare("GetTimeSeries", c, []byte(`{"range":{"start":"2020-01-01T00:00:00Z","end":"2026-10-17T14:30:00Z"}}`)); err == nil {
		t.Fatal("unbounded request")
	}
	c.Filters = map[string]string{"state": "resolved", "check": "server-001"}
	req, _, err = Prepare("ListFindings", c, nil)
	if err != nil || !strings.Contains(string(req), "FINDING_STATE_RESOLVED") || !strings.Contains(string(req), "server-001") {
		t.Fatalf("%s %v", req, err)
	}
}
func TestCommandErrorsAreStructured(t *testing.T) {
	for _, args := range [][]string{{"methods", "extra"}, {"describe", "MuteFinding"}, {"unknown"}, {"query"}, {"query", "MuteFinding"}, {"query", "ListMetrics"}, {"query", "ListMetrics", "-profiles", "/missing", "-profile", "fixture", "-timeout", "46s"}, {"query", "ListMetrics", "-profiles", "/missing", "-profile", "fixture", "-context", "-", "-request", "-"}} {
		var out bytes.Buffer
		err := Run(context.Background(), args, strings.NewReader(""), &out, &out)
		if err == nil || !strings.Contains(out.String(), "investigation_failed") {
			t.Fatalf("%v: %s %v", args, out.String(), err)
		}
	}
}

func TestFindingSourceFilterUsesAPIEnum(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/events?system=shop&src=findings", "https://lens.example.com", fixtureNow())
	req, unapplied, err := Prepare("ListEvents", c, nil)
	if err != nil || len(unapplied) > 0 || !strings.Contains(string(req), "EVENT_SOURCE_FINDING") {
		t.Fatalf("%s %v %v", req, unapplied, err)
	}
}

func TestRequestCannotDropFrozenRange(t *testing.T) {
	c, _ := ContextFromURL("https://lens.example.com/servers/nats-0?system=shop&range=1h", "https://lens.example.com", fixtureNow())
	if _, _, err := Prepare("GetTimeSeries", c, []byte(`{"metric":"server.cpu_percent","range":null}`)); err == nil {
		t.Fatal("null range silently restored a live server default")
	}
}

func TestPageDefaultFiltersSurviveHandoff(t *testing.T) {
	for _, test := range []struct{ path, method, want string }{{"/events", "ListEvents", `"hideRoutine":true`}, {"/findings", "ListFindings", `"includeMuted":true`}} {
		c, _ := ContextFromURL("https://lens.example.com"+test.path+"?system=shop", "https://lens.example.com", fixtureNow())
		req, _, err := Prepare(test.method, c, nil)
		if err != nil || !strings.Contains(strings.ReplaceAll(string(req), " ", ""), test.want) {
			t.Errorf("%s: %s %v", test.path, req, err)
		}
		if test.method == "ListEvents" && !strings.Contains(strings.ReplaceAll(string(req), " ", ""), `"foldRepeats":true`) {
			t.Errorf("event grouping lost: %s", req)
		}
	}
}
