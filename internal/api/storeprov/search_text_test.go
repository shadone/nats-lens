package storeprov_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// The text filter of ListFindings matches the check code, the check name
// and the entity, ignoring case; the other findings are left out.
func TestFindingsText(t *testing.T) {
	c, _ := findingsClient(t)
	for text, want := range map[string][]string{
		"":                {missingID, "stream-005:" + noLimitsKey},
		"CONSUMER-010":    {missingID},
		"job-cache":       {missingID},
		"catalog-changes": {"stream-005:" + noLimitsKey},
		"order-jobs":      {missingID},
		"no such thing":   nil,
	} {
		res := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour(), Text: text})))
		var got []string
		for _, f := range res.GetFindings() {
			got = append(got, f.GetId())
		}
		if strings.Join(got, " ") != strings.Join(want, " ") || int(res.GetTotalCount()) != len(want) {
			t.Errorf("%q: %v (total %d), want %v", text, got, res.GetTotalCount(), want)
		}
	}
	// A check's name matches too.
	name := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour(), Text: missingID[:12]}))).GetFindings()
	if len(name) != 1 {
		t.Fatalf("by code prefix: %v", name)
	}
	byName := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour(),
		Text: strings.ToUpper(name[0].GetCheckName())}))).GetFindings()
	if len(byName) != 1 || byName[0].GetId() != missingID {
		t.Fatalf("by check name %q: %v", name[0].GetCheckName(), byName)
	}
}

// The text filter of ListEvents matches the kind, the names an event
// carries and a finding event's finding ID, ignoring case and taking %
// and _ as themselves; facets and the routine count follow it.
func TestEventsText(t *testing.T) {
	st := emptyStore(t)
	finding := ev(0, store.SourceFinding, "finding.firing")
	finding.Account, finding.Stream = "APP", "orders"
	finding.Raw = json.RawMessage(`{"finding_id":"consumer-010:APP/orders/analytics","severity":"warning","detail":"d"}`)
	connect1 := ev(1, events.SourceSys, "client.connect")
	connect1.ClientName, connect1.ServerName = "Analytics-Worker", "nats-0"
	created := ev(2, events.SourceJetStream, "stream.created")
	created.Stream = "orders_100%"
	shutdown := ev(3, events.SourceSys, "server.shutdown")
	shutdown.ServerName = "nats-1"
	write(t, st, finding, connect1, created, shutdown)
	c := serveTooling(t, st)
	for text, want := range map[string]int32{
		"":             4,
		"analytics":    2,
		"CONSUMER-010": 1,
		"shutdown":     1,
		"nats-":        2,
		"100%":         1,
		"%":            1,
		"o_ders":       0,
		"none":         0,
	} {
		res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide(), Text: text})))
		if res.GetTotalCount() != want || int32(len(res.GetEvents())) != want {
			t.Errorf("%q: total %d, %d rows, want %d", text, res.GetTotalCount(), len(res.GetEvents()), want)
		}
	}
	rich := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: wide(), Text: "nats-", HideRoutine: true,
		IncludeFacets: true})))
	var servers []string
	for _, f := range rich.GetFacets() {
		if f.GetDimension() == "server" {
			servers = append(servers, f.GetValue())
		}
	}
	if rich.GetTotalCount()+rich.GetRoutineHidden() != 2 || rich.GetTotalUnfiltered() != 4 || len(servers) != 2 {
		t.Fatalf("rich: total %d, hidden %d, unfiltered %d, servers %v", rich.GetTotalCount(), rich.GetRoutineHidden(),
			rich.GetTotalUnfiltered(), servers)
	}
}

func TestTextTooLong(t *testing.T) {
	c := serveTooling(t, emptyStore(t))
	long := strings.Repeat("a", 257)
	if _, err := c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Text: long})); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("events: %v", err)
	}
	if _, err := c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Text: long})); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("findings: %v", err)
	}
	if _, err := c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Text: "  " + strings.Repeat("a", 256) + " "})); err != nil {
		t.Errorf("trimmed to 256: %v", err)
	}
}

// An event matches the ID of a finding it caused: a check code finds the
// consumer.deleted event behind a missing durable.
func TestEventsTextMatchesCausedFinding(t *testing.T) {
	c, deleted := findingsClient(t)
	res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: hour(), Text: "Consumer-010"})))
	found := false
	for _, e := range res.GetEvents() {
		found = found || e.GetId() == strconv.FormatInt(deleted, 10)
	}
	if !found {
		t.Fatalf("consumer.deleted %d not among %v", deleted, res.GetEvents())
	}
}
