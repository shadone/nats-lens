package fake_test

import (
	"cmp"
	"fmt"
	"testing"
)

// Each demo system's Accounts tile names the account its Accounts page
// puts first, with the page's count: clients plus the counted leaf nodes.
func TestAccountsTileAgreesWithPage(t *testing.T) {
	for _, sys := range []string{"example-prod", "example-global", "example-staging"} {
		tile := overviewTiles(t, sys)["accounts"]
		var want string
		for _, a := range accountsAndAuth(t, sys).GetAccounts() {
			if n := a.GetConnections() + a.GetLimitLeafnodes(); !a.GetIsSystem() && n > 0 {
				want = fmt.Sprintf("most connections: %s (%d)", cmp.Or(a.GetAccountName(), a.GetAccount()), n)
				break
			}
		}
		if tile.GetSubtitle() != want {
			t.Errorf("%s accounts subtitle %q, want %q", sys, tile.GetSubtitle(), want)
		}
	}
}
