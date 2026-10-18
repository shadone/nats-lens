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
		r, err := ResolveRange(m.GetRange(), sys.Provider.Now(), def)
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
	res.SystemKnown, res.DefaultSystem, res.Systems = known, d.Ref.GetId(), <-badges
	return connect.NewResponse(res), nil
}

// badges reads every system's badge status in parallel, each within
// SummaryTimeout, from its summary of the last 24 hours with one strip
// bucket. A summary that was read is kept for one collect interval; the
// runtime's collection status is applied afresh on every call.
func (h *Handler) badges(ctx context.Context, d System) []*v1.SystemStatus {
	now := d.Provider.Now()
	m := &v1.ListSystemsRequest{StripBuckets: 1, Range: &v1.TimeRange{Start: timestamppb.New(now.Add(-badgeRange)), End: timestamppb.New(now)}}
	key := summaryKey(m, true)
	timeout := cmp.Or(h.SummaryTimeout, DefaultSummaryTimeout)
	out := make([]*v1.SystemStatus, len(h.Systems))
	var wg sync.WaitGroup
	for i, sys := range h.Systems {
		wg.Go(func() {
			sum, ok := h.badgeCache.get(sys.Ref.GetId(), key)
			if !ok {
				sum = h.summary(ctx, sys, proto.CloneOf(m), key, timeout)
				if sum.GetError() == "" {
					h.badgeCache.put(sys.Ref.GetId(), key, sum)
				}
			} else if sys.Status != nil {
				withCollectionStatus(sum, sys.Status())
			}
			out[i] = badgeOf(sys.Ref, sum)
		})
	}
	wg.Wait()
	return out
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

// badgeCache keeps each system's badge summary for one collect interval,
// so that shells polling together read each provider once per interval.
type badgeCache struct {
	mu sync.Mutex
	m  map[string]badgeEntry
}

type badgeEntry struct {
	key     string
	sum     *v1.SystemSummary
	expires time.Time
}

// get returns a copy of the system's summary for the key while it is
// kept.
func (c *badgeCache) get(id, key string) (*v1.SystemSummary, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok || e.key != key || !time.Now().Before(e.expires) {
		return nil, false
	}
	return proto.CloneOf(e.sum), true
}

// put keeps a copy of the summary for its freshness's collect interval.
func (c *badgeCache) put(id, key string, sum *v1.SystemSummary) {
	ttl := cmp.Or(sum.GetFreshness().GetInterval().AsDuration(), defaultInterval)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]badgeEntry{}
	}
	c.m[id] = badgeEntry{key: key, sum: proto.CloneOf(sum), expires: time.Now().Add(ttl)}
}
