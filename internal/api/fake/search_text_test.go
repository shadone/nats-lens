package fake_test

import (
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The demo answers the command bar's searches: a check code and a
// consumer's name find its finding and its events.
func TestDemoText(t *testing.T) {
	c := client(t)
	for _, text := range []string{"consumer-010", "Analytics"} {
		fs := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Text: text, IncludeMuted: true}))).GetFindings()
		if len(fs) != 1 || fs[0].GetCheckCode() != "consumer-010" {
			t.Errorf("%q: findings %v", text, fs)
		}
		evs := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Text: text, PageSize: 5})))
		if evs.GetTotalCount() == 0 {
			t.Errorf("%q: no events", text)
		}
		for _, e := range evs.GetEvents() {
			if e.GetConsumer() != "analytics-aggregator" && e.GetClientName() != "analytics-worker" &&
				e.GetTransition().GetFindingId() == "" {
				t.Errorf("%q: unrelated event %v", text, e)
			}
		}
	}
	none := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Text: "no such name"})))
	if none.GetTotalCount() != 0 {
		t.Errorf("unmatched text: %d events", none.GetTotalCount())
	}
}

// A Kubernetes event is found by the reason its row shows and by its
// object, namespace/name, which only its summary carries.
func TestDemoTextKubernetes(t *testing.T) {
	c := client(t)
	for text, want := range map[string]int32{"Unhealthy": 3, "failedscheduling": 5, "shop/resize-worker": 8} {
		if got := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Text: text}))).GetTotalCount(); got != want {
			t.Errorf("%q: %d events, want %d", text, got, want)
		}
	}
}
