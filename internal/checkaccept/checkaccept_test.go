package checkaccept_test

import (
	"testing"

	"github.com/shadone/nats-lens/internal/checkaccept"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
)

// The built-in catalog marks the checks of a configuration choice as
// acceptable, and the two bounded by growth with the growth review.
func TestBuiltinAcceptance(t *testing.T) {
	for _, c := range []string{"stream-005", "stream-018", "stream-019", "stream-020", "consumer-016", "consumer-017"} {
		if !checkaccept.Acceptable(c) {
			t.Errorf("%s is not acceptable", c)
		}
		if want := c == "stream-005" || c == "stream-019" || c == "stream-020"; checkaccept.GrowthReview(c) != want {
			t.Errorf("%s growth review = %v, want %v", c, checkaccept.GrowthReview(c), want)
		}
	}
	for _, c := range []string{"stream-001", "stream-002", "consumer-002", "server-014", "client-007", "account-003", "no-such-999"} {
		if checkaccept.Acceptable(c) || checkaccept.GrowthReview(c) {
			t.Errorf("%s is acceptable", c)
		}
	}
}

// The package reads the same keys the check engine validates.
func TestAgreesWithTheEngine(t *testing.T) {
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range set.Checks() {
		if checkaccept.Acceptable(c.Code) != c.Acceptable || checkaccept.GrowthReview(c.Code) != (c.AcceptReview == checks.AcceptReviewGrowth) {
			t.Errorf("%s: acceptable %v review %q; checkaccept says %v, %v", c.Code, c.Acceptable, c.AcceptReview,
				checkaccept.Acceptable(c.Code), checkaccept.GrowthReview(c.Code))
		}
	}
}
