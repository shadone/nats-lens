package investigation

import (
	"encoding/json"
	"testing"
)

func TestR2URLConsumerStreamOwnsAccount(t *testing.T) {
	c, err := ContextFromURL("https://lens.example.com/consumers?system=shop&account=OTHER&stream=APP/orders", "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		req, _, err := Prepare("ListConsumers", c, nil)
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Account string `json:"account"`
			Stream  string `json:"stream"`
		}
		json.Unmarshal(req, &r)
		counts[r.Account]++
	}
	if counts["OTHER"] > 0 {
		t.Fatalf("UI selected stream owns APP account, but identical supported URL produces request accounts: %v", counts)
	}
}
