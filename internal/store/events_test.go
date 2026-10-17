package store

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/events"
)

func sampleEvent(at time.Time, kind string) events.Event {
	return events.Event{
		Time: at, ReceivedAt: at.Add(time.Millisecond), Source: events.SourceJetStream,
		Type: "io.nats.jetstream.advisory.v1.consumer_action", Kind: kind, Account: "APP",
		Stream: "order-jobs", Consumer: "resize-worker", ClientName: "test-deleter",
		Summary: "consumer deleted", Raw: json.RawMessage(`{"action":"delete"}`),
	}
}

func TestWriteEventsAndLastActivity(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if last, err := s.LastActivity(ctx); err != nil || !last.IsZero() {
		t.Fatalf("empty database: last = %v, err = %v", last, err)
	}
	if err := s.WriteEvents(ctx, []events.Event{sampleEvent(t0, "consumer.deleted"), sampleEvent(t0.Add(time.Second), "consumer.created")}); err != nil {
		t.Fatal(err)
	}
	var kind, client, raw string
	var typ *string
	if err := s.DB().QueryRow(`SELECT kind, client_name, type, raw::VARCHAR FROM events ORDER BY id LIMIT 1`).
		Scan(&kind, &client, &typ, &raw); err != nil {
		t.Fatal(err)
	}
	if kind != "consumer.deleted" || client != "test-deleter" || typ == nil || raw != `{"action":"delete"}` {
		t.Fatalf("row = %s %s %v %s", kind, client, typ, raw)
	}
	last, err := s.LastActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(time.Second + time.Millisecond); !last.Equal(want) {
		t.Fatalf("last activity = %v, want %v", last, want)
	}
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if last, _ := s.LastActivity(ctx); !last.After(t0.Add(time.Minute)) {
		t.Fatalf("last activity should follow the epoch: %v", last)
	}
}

func TestSweepDeletesOldEvents(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	old, recent := t0.Add(-20*24*time.Hour), t0.Add(-time.Hour)
	if err := s.WriteEvents(ctx, []events.Event{sampleEvent(old, "consumer.deleted"), sampleEvent(recent, "consumer.created")}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Sweep(ctx, t0, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 1 || count(t, s.DB(), `SELECT count(*) FROM events`) != 1 {
		t.Fatalf("sweep = %+v", res)
	}
}

// Epoch and event writes from different goroutines must both succeed.
func TestConcurrentEpochAndEventWrites(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 10 {
		wg.Go(func() {
			_, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*time.Minute)))
			errs <- err
		})
		wg.Go(func() {
			errs <- s.WriteEvents(ctx, []events.Event{sampleEvent(t0, "consumer.nak"), sampleEvent(t0, "consumer.nak")})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM events`); n != 20 {
		t.Fatalf("events = %d", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM epochs`); n != 10 {
		t.Fatalf("epochs = %d", n)
	}
}

// A string the driver cannot bind must not fail the batch: a failed batch
// is retried and would block every later event.
func TestWriteEventsWithBadStrings(t *testing.T) {
	ctx := context.Background()
	for name, mut := range map[string]func(*events.Event){
		"nul":          func(e *events.Event) { e.ClientName = "a\x00b"; e.Summary = "client a\x00b" },
		"invalid utf8": func(e *events.Event) { e.ClientName = "a\xffb"; e.Reason = "\xc3"; e.Summary = "s\xff" },
		"raw":          func(e *events.Event) { e.Raw = json.RawMessage("{\"client\":{\"name\":\"a\xffb\"}}") },
	} {
		t.Run(name, func(t *testing.T) {
			s := openMem(t)
			e := sampleEvent(t0, "client.connect")
			mut(&e)
			if err := s.WriteEvents(ctx, []events.Event{e, sampleEvent(t0, "client.disconnect")}); err != nil {
				t.Fatalf("batch failed: %v", err)
			}
			if n := count(t, s.DB(), `SELECT count(*) FROM events`); n != 2 {
				t.Fatalf("events = %d", n)
			}
		})
	}
}
