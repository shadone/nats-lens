package investigation

import (
	"strings"
	"testing"
)

func TestFindingOccurrenceReadAdmission(t *testing.T) {
	if Method("ListFindingEpisodes") == nil || !AllowedProcedure("/"+Service+"/ListFindingEpisodes") {
		t.Fatal("bounded finding history is unavailable on the read-only source")
	}
	c, err := ContextFromURL("https://lens.example.com/findings/stream-001%3AAPP%2Forders?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z", "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	req, _, err := Prepare("ListFindingEpisodes", c, []byte(`{"findingId":"stream-001:APP/orders","firstRetainedEpoch":"9007199254740993","readEpoch":"9007199254740995","includeTransitions":true,"pageSize":1}`))
	if err != nil || !strings.Contains(string(req), `9007199254740993`) {
		t.Fatalf("exact occurrence preparation: %s %v", req, err)
	}
}

func TestFindingOccurrenceURLImportsExactRead(t *testing.T) {
	c, err := ContextFromURL("https://lens.example.com/findings/stream-001%3AAPP%2Forders/occurrences/9007199254740993?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z&readEpoch=9007199254740995", "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	if c.Query == nil || c.Query.Method != "ListFindingEpisodes" {
		t.Fatalf("exact occurrence query lost: %+v", c)
	}
	req, _, err := Prepare("ListFindingEpisodes", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{`9007199254740993`, `9007199254740995`, `stream-001:APP/orders`, `"includeTransitions":true`} {
		if !strings.Contains(string(req), v) {
			t.Fatalf("lost %s: %s", v, req)
		}
	}
}
