package notify

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// Delivery owns one sender per channel of one system. Run must be called once.
// Channels run independently, so a slow receiver cannot hold back another.
type Delivery struct {
	Store                                *store.Store
	SystemID, SystemName, Version, UIURL string
	Channels                             []config.Channel
	Interval                             time.Duration
	Clock                                func() time.Time
	Log                                  *slog.Logger
}

func (d *Delivery) now() time.Time {
	if d.Clock != nil {
		return d.Clock()
	}
	return time.Now()
}

// Step attempts only the oldest batch, returning the time to wait. Exposed for
// deterministic clock tests; callers must serialize it per channel.
func (d *Delivery) Step(ctx context.Context, ch config.Channel) (time.Duration, error) {
	s := NewSender(ch, d.Version)
	defer s.Close()
	return d.step(ctx, ch, s)
}
func (d *Delivery) step(ctx context.Context, ch config.Channel, s *Sender) (time.Duration, error) {
	now := d.now()
	rows, at, err := d.Store.DeliveryBatch(ctx, ch.Name, now, 100)
	if err != nil || len(rows) == 0 {
		return time.Second, err
	}
	head := rows[0]
	if head.NextAttemptAt.After(now) {
		return head.NextAttemptAt.Sub(now), nil
	}
	b := Batch{SystemID: d.SystemID, SystemName: d.SystemName, Version: d.Version, UIURL: d.UIURL, SentAt: at, Key: fmt.Sprintf("%s:%s:%d:%d", d.SystemID, ch.Name, head.ID, rows[len(rows)-1].ID)}
	for _, r := range rows {
		m, err := Decode(r.Message)
		if err != nil {
			return time.Second, fmt.Errorf("decode outbox row %d", r.ID)
		}
		b.Messages = append(b.Messages, m.WithLink(d.UIURL, d.SystemID))
	}
	result := s.Send(ctx, b)
	// A shutdown cancellation is not a receiver failure. Leave the batch intact.
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	now = d.now()
	state := store.OutboxPending
	wait := 10 * time.Second
	for i := 0; i < head.Attempts && wait < 10*time.Minute; i++ {
		wait = min(wait*2, 10*time.Minute)
	}
	if (result.Status == 429 || result.Status == 503) && result.RetryAfter > 0 {
		wait = min(result.RetryAfter, 10*time.Minute)
	}
	switch result.Outcome {
	case Delivered:
		state = store.OutboxDelivered
	case Refused:
		state = store.OutboxRefused
	case Retry:
		if now.Sub(head.CreatedAt) >= ch.GiveUpAfter.D() {
			state = store.OutboxFailed
		}
	}
	var next time.Time
	if state == store.OutboxPending {
		next = now.Add(wait)
	} else {
		wait = max(d.Interval, time.Second)
	}
	// ASVS 13.3.1: persist only status codes, never destination or error text.
	if err = d.Store.FinishDelivery(ctx, ch.Name, head.ID, rows[len(rows)-1].ID, state, result.Status, now, next); err != nil {
		return time.Second, err
	}
	if d.Log != nil {
		d.Log.Info("notification delivery", "channel", ch.Name, "host", result.Host, "state", state, "status", result.Status)
	}
	return wait, nil
}

// Run stops scheduling at stop's cancellation. An in-flight request keeps the
// supplied request context, whose deadline is the existing shutdown grace.
func (d *Delivery) Run(stop, requests context.Context) {
	var wg sync.WaitGroup
	for _, ch := range d.Channels {
		if len(ch.Systems) > 0 && !slices.Contains(ch.Systems, d.SystemID) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewSender(ch, d.Version)
			defer s.Close()
			for {
				if stop.Err() != nil || requests.Err() != nil {
					return
				}
				wait, err := d.step(requests, ch, s)
				if err != nil {
					if requests.Err() != nil {
						return
					}
					if d.Log != nil {
						d.Log.Error("notification outbox unavailable", "channel", ch.Name)
					}
					wait = time.Second
				}
				t := time.NewTimer(wait)
				select {
				case <-stop.Done():
					t.Stop()
					return
				case <-requests.Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
		}()
	}
	wg.Wait()
}
