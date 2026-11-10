package fake

import (
	"context"
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// In operator mode an event names its account by key; the text filter
// sees the account's readable name on a copy, and the shared scenario
// event stays as it was.
func TestTextSeesAccountName(t *testing.T) {
	p := newProvider(globalScenario(), false)
	e := &v1.Event{Kind: "stream.created", Account: SuppliersAccount, Stream: "invoices"}
	other := &v1.Event{Kind: "stream.created", Account: Account}
	named := p.namedAccounts(context.Background(), []*v1.Event{e, other})
	if named[0].GetAccountName() != "SUPPLIERS" || named[1] != other {
		t.Fatalf("named %v", named)
	}
	if e.GetAccountName() != "" {
		t.Fatalf("scenario event changed: %v", e)
	}
}
