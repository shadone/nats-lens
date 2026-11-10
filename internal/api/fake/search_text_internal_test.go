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

// The fake reads a Kubernetes event's object from its payload as storeprov
// does (TestEventsTextKubernetesObjects there has the same cases).
func TestTextKubernetesObjects(t *testing.T) {
	k8s := func(raw string) *v1.Event { return &v1.Event{Source: srcK8s, Kind: "k8s.event", Raw: raw} }
	node := k8s(`{"node":"gke-pool-1-c40d","pool":"pool-1"}`)
	pod := k8s(`{"object":{"kind":"Pod","namespace":"natsns","name":"nats-0"},"node":"gke-pool-2-aaaa"}`)
	noName := k8s(`{"object":{"kind":"X","namespace":"onlyns"}}`)
	bad := k8s(`{"object":"qqbad"}`)
	podWatch := k8s(`{"namespace":"shop","pod":"indexer-7f","node":"gke-pool-3-bbbb"}`)
	for _, c := range []struct {
		e    *v1.Event
		text string
		want bool
	}{
		{node, "gke-pool-1-c40d", true}, {pod, "natsns/nats-0", true}, {pod, "gke-pool-2", false},
		{noName, "onlyns", true}, {bad, "qqbad", false}, {podWatch, "shop/indexer-7f", true}, {podWatch, "gke-pool-3", false},
	} {
		if got := keepEvent(c.e, &v1.ListEventsRequest{Text: c.text}); got != c.want {
			t.Errorf("%s %q: %v, want %v", c.e.GetRaw(), c.text, got, c.want)
		}
	}
}
