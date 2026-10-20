package api_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A connection that captures events is lost from its first failure to its
// next reconnect; the collector's own connection is no such connection.
func TestEventsConnectionGaps(t *testing.T) {
	conn := func(at time.Time, kind, label string) *v1.Event {
		return ev(at, kind, "", func(e *v1.Event) { e.Raw = `{"connection":"` + label + `","reason":"x"}` })
	}
	gaps := api.EventsConnectionGaps([]*v1.Event{
		conn(rAt(1, 0), "collector.not_connected", "collector"),
		conn(rAt(2, 0), "collector.not_connected", "sys"),
		conn(rAt(12, 0), "collector.not_connected", "sys"),
		conn(rAt(15, 0), "collector.reconnected", "sys"),
		conn(rAt(20, 0), "collector.connection_closed", "APP"),
	})
	if len(gaps) != 2 || !gaps[0].Start.Equal(rAt(2, 0)) || !gaps[0].End.Equal(rAt(15, 0)) || !gaps[1].Start.Equal(rAt(20, 0)) ||
		!gaps[1].End.IsZero() || gaps[0].Kind != api.GapEvents {
		t.Errorf("gaps = %+v", gaps)
	}
}

// A disconnect opens the time without events at once (core subscriptions
// lose what comes while disconnected), and nats-lens starting again ends
// every open one: its connections are new.
func TestEventsConnectionGapsOpenAtDisconnectAndEndAtStart(t *testing.T) {
	conn := func(at time.Time, kind, label string) *v1.Event {
		return ev(at, kind, "", func(e *v1.Event) { e.Raw = `{"connection":"` + label + `","reason":"x"}` })
	}
	gaps := api.EventsConnectionGaps([]*v1.Event{
		conn(rAt(1, 0), "collector.disconnected", "sys"),
		conn(rAt(1, 10), "collector.reconnected", "sys"),
		conn(rAt(5, 0), "collector.not_connected", "APP"),
		ev(rAt(9, 0), "collector.started", ""),
	})
	if len(gaps) != 2 || !gaps[0].Start.Equal(rAt(1, 0)) || !gaps[0].End.Equal(rAt(1, 10)) || !gaps[1].Start.Equal(rAt(5, 0)) ||
		!gaps[1].End.Equal(rAt(9, 0)) {
		t.Errorf("gaps = %+v", gaps)
	}
}
