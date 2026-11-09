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
