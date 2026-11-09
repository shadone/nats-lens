package fake

import (
	"fmt"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type eventCapture struct {
	scen     scen
	events   []*v1.Event
	boundary int64
}

// The demo uses the same whole-second reference-frame convention as storage
// comparisons and occurrence history. It never shifts a selected event on replay.
func (p *Provider) captureEvents(boundary *int64) (eventCapture, error) {
	now := p.Now()
	s := scen{now: now, shift: now.Sub(FixedNow), sc: p.sc}
	if _, err := api.EventSelection(nil, boundary); err != nil {
		return eventCapture{}, err
	}
	if boundary != nil && *boundary == 0 {
		return eventCapture{scen: s}, nil
	}
	upper := int64(-1)
	if boundary != nil {
		frame := *boundary / storageFrameScale
		upper = *boundary % storageFrameScale
		if frame <= 0 {
			return eventCapture{}, api.ErrInvalid
		}
		at := time.Unix(frame, 0).UTC()
		if at.After(now) {
			return eventCapture{}, api.ErrInvalid
		}
		if at.Before(s.dataStart()) {
			return eventCapture{}, fmt.Errorf("%w: demo event reference frame expired", api.ErrFailedPrecondition)
		}
		s = scen{now: at, shift: at.Sub(FixedNow), sc: p.sc}
	}
	events := p.events(s)
	highest := int64(0)
	for _, e := range events {
		highest = max(highest, num(e.Id))
	}
	if highest >= storageFrameScale {
		return eventCapture{}, fmt.Errorf("%w: demo event identities exceed frame budget", api.ErrResourceExhausted)
	}
	if upper > highest {
		return eventCapture{}, fmt.Errorf("%w: demo event boundary unavailable", api.ErrFailedPrecondition)
	}
	if upper < 0 {
		upper = highest
	}
	events = slices.DeleteFunc(events, func(e *v1.Event) bool { return num(e.Id) > upper })
	readID := s.now.Unix()*storageFrameScale + upper
	if boundary != nil {
		readID = *boundary
	}
	return eventCapture{scen: s, events: events, boundary: readID}, nil
}

func selectedDemoEvents(events []*v1.Event, ids []string) []*v1.Event {
	if len(ids) == 0 {
		return events
	}
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	return slices.DeleteFunc(events, func(e *v1.Event) bool { return !selected[e.Id] })
}
