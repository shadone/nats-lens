package fake

import (
	"testing"
)

// example-global's duplicate subscriptions: search-api's plain one fires
// client-009; inventory-service holds its queue subscription four times
// per connection, as a worker pool does, and does not.
func TestGlobalDuplicateSubscriptions(t *testing.T) {
	p := newProvider(globalScenario(), false)
	s := p.scen()
	subs, _, _ := s.newestLists()
	queued := 0
	for _, x := range subs {
		if x.Subject == "inventory.reserve" && x.Queue == "inventory-service" && x.Subs == 4 {
			queued++
		}
	}
	if queued == 0 {
		t.Fatal("no inventory-service connection holds inventory.reserve four times")
	}
	var keys []string
	for _, f := range s.duplicateSubscriptions(s.lastEpoch()) {
		keys = append(keys, f.GetEntity().GetKey())
	}
	if len(keys) != 1 || keys[0] != "search-api" {
		t.Errorf("client-009 on %v, want search-api alone", keys)
	}
}
