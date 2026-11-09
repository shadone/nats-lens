package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Ranges of GetShellStatus.
const (
	// shellIntervals is the span of an unset range, in collect intervals.
	shellIntervals = 2
	// badgeRange is the range every system's badge status covers: the
	// default live range the Fleet page opens with.
	badgeRange = 24 * time.Hour
	// defaultInterval is the collect interval of a provider or summary
	// that names none.
	defaultInterval = 30 * time.Second
	// badgeWait is the longest a call waits for a system's badge summary
	// when none is kept.
	badgeWait = 500 * time.Millisecond
	// DefaultShellTimeout bounds the system's part of GetShellStatus.
	DefaultShellTimeout = 10 * time.Second
)

// GetShellStatus returns the system's freshness, servers and finding tally
// with every system's badge status. An unknown system is not an error: the
// response lists the systems with system_known false, and the system's
// part is not read. A failure of the system's part, or one that runs past
// ShellTimeout, is reported in the response's error with the systems still
// listed, so the shell keeps its system list and badges. Unlike call, it does not look up account names, as the
// response names no account.
func (h *Handler) GetShellStatus(ctx context.Context, req *connect.Request[v1.GetShellStatusRequest]) (*connect.Response[v1.GetShellStatusResponse], error) {
	const procedure = "GetShellStatus"
	d, ok := h.Systems.Default()
	if !ok {
		return nil, h.codeOf(procedure, fmt.Errorf("%w: no system is configured", ErrFailedPrecondition))
	}
	m := req.Msg
	sys, err := h.Systems.Resolve(m.GetSystem())
	if err != nil && !errors.Is(err, ErrInvalid) {
		return nil, h.codeOf(procedure, err)
	}
	known := err == nil
	if known {
		def := shellIntervals * cmp.Or(sys.Provider.CollectInterval(), defaultInterval)
		r, err := ResolveRange(m.GetRange(), rangeReference(sys.Provider), def)
		if err != nil {
			return nil, h.codeOf(procedure, err)
		}
		m.Range = r
	}
	badges := make(chan []*v1.SystemStatus, 1)
	go func() { badges <- h.badges(ctx, d) }()
	res := &v1.GetShellStatusResponse{}
	if known {
		qctx, cancel := context.WithTimeout(ctx, cmp.Or(h.ShellTimeout, DefaultShellTimeout))
		defer cancel()
		part, err := sys.Provider.ShellStatus(qctx, m)
		switch {
		case ctx.Err() != nil:
			// The caller went away or ran out of time; nothing failed here.
			return nil, h.codeOf(procedure, ctx.Err())
		case err != nil:
			var ce *connect.Error
			if errors.As(h.codeOf(procedure, err), &ce) {
				res.ErrorCode, res.Error = ce.Code().String(), ce.Message()
			}
		default:
			res = part
			if sys.Status != nil {
				withCollecting(res.GetFreshness(), sys.Status())
			}
		}
	}
	systems := <-badges
	if ctx.Err() != nil {
		// The badges gave up waiting when the caller's time ran out; the
		// call ends as any call past its deadline does.
		return nil, h.codeOf(procedure, ctx.Err())
	}
	res.SystemKnown, res.DefaultSystem, res.Systems = known, d.Ref.GetId(), systems
	return connect.NewResponse(res), nil
}

// badges returns every system's badge status, from its summary of the
// last 24 hours with one strip bucket. A summary that was read is kept for
// one collect interval and served at once; once it expires it is still
// served while one read in the background replaces it. Only a system with
// no summary kept, or whose last read failed, waits for its read, and at
// most badgeWait: past that its badge is pending. The runtime's collection
// status is applied afresh on every call.
func (h *Handler) badges(ctx context.Context, d System) []*v1.SystemStatus {
	now := rangeReference(d.Provider)
	m := &v1.ListSystemsRequest{StripBuckets: 1, Range: &v1.TimeRange{Start: timestamppb.New(now.Add(-badgeRange)), End: timestamppb.New(now)}}
	key := summaryKey(m, true)
	timeout := cmp.Or(h.SummaryTimeout, DefaultSummaryTimeout)
	wait := cmp.Or(h.badgeWait, badgeWait)
	out := make([]*v1.SystemStatus, len(h.Systems))
	var wg sync.WaitGroup
	for i, sys := range h.Systems {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					h.warnSummary(sys, fmt.Errorf("panic: %v", r))
					out[i] = &v1.SystemStatus{System: proto.CloneOf(sys.Ref), Error: "the status could not be read; see the server log"}
				}
			}()
			id := sys.Ref.GetId()
			kept, ok, start, done := h.badgeCache.lookup(id)
			if start {
				go h.refreshBadge(sys, proto.CloneOf(m), key, timeout)
			}
			if !ok {
				t := time.NewTimer(wait)
				defer t.Stop()
				select {
				case <-done:
					kept = h.badgeCache.peek(id)
				case <-t.C:
				case <-ctx.Done():
				}
			}
			if kept == nil {
				out[i] = &v1.SystemStatus{System: proto.CloneOf(sys.Ref), Pending: true}
				return
			}
			if sys.Status != nil && kept.GetError() == "" {
				withCollectionStatus(kept, sys.Status())
			}
			out[i] = badgeOf(sys.Ref, kept)
		})
	}
	wg.Wait()
	return out
}

// refreshBadge reads a system's badge summary and keeps the result; no
// caller's context bounds it, only the summary's timeout.
func (h *Handler) refreshBadge(sys System, req *v1.ListSystemsRequest, key string, timeout time.Duration) {
	var sum *v1.SystemSummary
	defer func() {
		if r := recover(); r != nil {
			h.warnSummary(sys, fmt.Errorf("panic: %v", r))
			sum = &v1.SystemSummary{Error: "the summary could not be read; see the server log"}
		}
		h.badgeCache.finish(sys.Ref.GetId(), sum)
	}()
	sum = h.summary(context.Background(), sys, req, key, timeout)
}

// badgeOf is the badge status of a system's summary. The system does not
// collect when its collector's connection fails or its freshness says so,
// as the Fleet page reads a summary.
func badgeOf(ref *v1.SystemRef, sum *v1.SystemSummary) *v1.SystemStatus {
	st := &v1.SystemStatus{System: proto.CloneOf(ref), Error: sum.GetError()}
	if st.Error != "" {
		return st
	}
	st.Status, st.Collection = sum.GetStatus(), sum.GetCollection()
	switch {
	case sum.GetCollection() == v1.CollectionState_COLLECTION_STATE_NOT_CONNECTED,
		sum.GetCollection() == v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED,
		sum.GetCollectionError() != "":
	default:
		st.Collecting = sum.GetFreshness() == nil || sum.GetFreshness().GetCollecting()
	}
	return st
}

// badgeCache keeps each system's badge summary, and the read that
// replaces it, so that shells polling together read each provider once per
// collect interval.
type badgeCache struct {
	mu sync.Mutex
	m  map[string]*badgeEntry
	// now is the clock of the expiry; nil is the wall clock.
	now func() time.Time
}

type badgeEntry struct {
	// sum is the newest summary read, an error card when the newest read
	// failed; nil before the first read ends.
	sum     *v1.SystemSummary
	expires time.Time
	// reading is closed when the read in flight ends; nil without one.
	reading chan struct{}
}

func (c *badgeCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// lookup returns a copy of the system's kept summary, and ok when it was
// read without an error, expired or not. When none is kept, it expired or
// it is an error card, and no read runs, it marks a read as running and
// returns start, and the caller starts it. done is closed when the read in
// flight ends; nil without one.
func (c *badgeCache) lookup(id string) (sum *v1.SystemSummary, ok, start bool, done <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*badgeEntry{}
	}
	e := c.m[id]
	if e == nil {
		e = &badgeEntry{}
		c.m[id] = e
	}
	ok = e.sum != nil && e.sum.GetError() == ""
	if (!ok || !c.clock().Before(e.expires)) && e.reading == nil {
		e.reading, start = make(chan struct{}), true
	}
	return proto.CloneOf(e.sum), ok, start, e.reading
}

// peek returns a copy of the system's kept summary; nil when none is.
func (c *badgeCache) peek(id string) *v1.SystemSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.m[id]; e != nil {
		return proto.CloneOf(e.sum)
	}
	return nil
}

// finish keeps a read's summary, for its freshness's collect interval when
// it was read, and ends the read.
func (c *badgeCache) finish(id string, sum *v1.SystemSummary) {
	ttl := cmp.Or(sum.GetFreshness().GetInterval().AsDuration(), defaultInterval)
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[id]
	if sum != nil {
		e.sum, e.expires = proto.CloneOf(sum), c.clock().Add(ttl)
	}
	close(e.reading)
	e.reading = nil
}
