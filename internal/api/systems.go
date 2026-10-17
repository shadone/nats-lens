package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Strip and summary limits of ListSystems.
const (
	// DefaultStripBuckets is the number of strip buckets when a request
	// sets none.
	DefaultStripBuckets = 48
	// MaxStripBuckets is the most strip buckets a request may ask for.
	MaxStripBuckets = 500
	// DefaultSummaryTimeout bounds each system's summary.
	DefaultSummaryTimeout = 5 * time.Second
)

// DefaultSystemID is the ID of the one system of a configuration that
// does not list systems.
const DefaultSystemID = "default"

// CollectionState is what a system's collector is doing.
type CollectionState string

// Collection states.
const (
	CollectionStarting         CollectionState = "starting"
	CollectionCollecting       CollectionState = "collecting"
	CollectionNotConnected     CollectionState = "not_connected"
	CollectionConnectionClosed CollectionState = "connection_closed"
)

// CollectionStatus is the state of a system's collector as the runtime
// sees it.
type CollectionStatus struct {
	State CollectionState
	// LastError is the newest connection error; empty when there is none.
	LastError string
	// Since is when State began.
	Since time.Time
	// LastEpochAt is when the newest epoch started; zero before the first.
	LastEpochAt time.Time
}

// System is one NATS system the API serves.
type System struct {
	// Ref names the system; its ID is the system field of a request.
	Ref *v1.SystemRef
	// Provider answers the calls for the system.
	Provider Provider
	// Status reports the system's collector; nil when there is no
	// collector, as in the demo.
	Status func() CollectionStatus
}

// Systems are the systems the API serves, in configuration order. The
// first is the default: a request with an empty system reads it.
type Systems []System

// One returns the registry of a single system.
func One(id, name string, p Provider) Systems {
	return Systems{{Ref: &v1.SystemRef{Id: id, Name: name}, Provider: p}}
}

// Default is the first system; ok is false when there is none.
func (s Systems) Default() (System, bool) {
	if len(s) == 0 {
		return System{}, false
	}
	return s[0], true
}

// IDs lists the system IDs in order.
func (s Systems) IDs() []string {
	out := make([]string, len(s))
	for i, sys := range s {
		out[i] = sys.Ref.GetId()
	}
	return out
}

// Resolve returns the system with the ID, or the default system for an
// empty ID. An unknown ID is ErrInvalid, naming the known IDs.
func (s Systems) Resolve(id string) (System, error) {
	if id == "" {
		if d, ok := s.Default(); ok {
			return d, nil
		}
		return System{}, fmt.Errorf("%w: no system is configured", ErrFailedPrecondition)
	}
	for _, sys := range s {
		if sys.Ref.GetId() == id {
			return sys, nil
		}
	}
	return System{}, fmt.Errorf("%w: unknown system %q; known systems: %s", ErrInvalid, id, strings.Join(s.IDs(), ", "))
}

// ListSystems lists the systems in order with a summary from each
// provider. The providers run in parallel, each within SummaryTimeout; a
// provider that fails, panics or runs out of time is listed with its
// reference and an error, and the others are not held up. A system's
// collection status, when the runtime reports one, replaces the
// collection state the provider read from stored events.
func (h *Handler) ListSystems(ctx context.Context, req *connect.Request[v1.ListSystemsRequest]) (*connect.Response[v1.ListSystemsResponse], error) {
	d, ok := h.Systems.Default()
	if !ok {
		return nil, h.codeOf("ListSystems", fmt.Errorf("%w: no system is configured", ErrFailedPrecondition))
	}
	m := req.Msg
	if err := withRange(d.Provider, &m.Range); err != nil {
		return nil, h.codeOf("ListSystems", err)
	}
	switch n := m.GetStripBuckets(); {
	case n == 0:
		m.StripBuckets = DefaultStripBuckets
	case n < 0 || n > MaxStripBuckets:
		return nil, h.codeOf("ListSystems", invalid("strip_buckets must be between 1 and %d", MaxStripBuckets))
	}
	timeout := cmp.Or(h.SummaryTimeout, DefaultSummaryTimeout)
	res := &v1.ListSystemsResponse{Range: m.GetRange(), DefaultSystem: d.Ref.GetId(), Systems: make([]*v1.SystemSummary, len(h.Systems))}
	var wg sync.WaitGroup
	for i, sys := range h.Systems {
		wg.Go(func() {
			// Each provider reads its own copy of the request.
			sum := h.summary(ctx, sys, proto.CloneOf(m), timeout)
			sum.System = proto.CloneOf(sys.Ref)
			if sys.Status != nil {
				withCollectionStatus(sum, sys.Status())
			}
			res.Systems[i] = sum
		})
	}
	wg.Wait()
	return connect.NewResponse(res), nil
}

// summary reads one system's summary within the timeout. The provider runs
// in its own goroutine, so that one that ignores its context does not hold
// up the response; its result is then dropped.
func (h *Handler) summary(ctx context.Context, sys System, req *v1.ListSystemsRequest, timeout time.Duration) *v1.SystemSummary {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		sum *v1.SystemSummary
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- result{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		sum, err := sys.Provider.SystemSummary(ctx, req)
		done <- result{sum, err}
	}()
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		r.err = ctx.Err()
	}
	switch {
	case errors.Is(r.err, context.DeadlineExceeded):
		h.warnSummary(sys, r.err)
		return &v1.SystemSummary{Error: fmt.Sprintf("the summary took longer than %s", timeout)}
	case r.err != nil:
		h.warnSummary(sys, r.err)
		return &v1.SystemSummary{Error: "the summary could not be read; see the server log"}
	case r.sum == nil:
		return &v1.SystemSummary{}
	}
	return r.sum
}

func (h *Handler) warnSummary(sys System, err error) {
	if h.Log != nil {
		h.Log.Warn("system summary failed", "system", sys.Ref.GetId(), "err", err)
	}
}

// collectionStates maps the runtime's states to the API's.
var collectionStates = map[CollectionState]v1.CollectionState{
	CollectionStarting:         v1.CollectionState_COLLECTION_STATE_STARTING,
	CollectionCollecting:       v1.CollectionState_COLLECTION_STATE_COLLECTING,
	CollectionNotConnected:     v1.CollectionState_COLLECTION_STATE_NOT_CONNECTED,
	CollectionConnectionClosed: v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED,
}

// withCollectionStatus sets the summary's collection fields from the
// runtime's status. The error and its time are kept only while the
// collector is not collecting.
func withCollectionStatus(sum *v1.SystemSummary, st CollectionStatus) {
	sum.Collection = collectionStates[st.State]
	sum.CollectionError, sum.CollectionErrorSince = "", nil
	if st.State == CollectionNotConnected || st.State == CollectionConnectionClosed {
		sum.CollectionError = st.LastError
		if !st.Since.IsZero() {
			sum.CollectionErrorSince = timestamppb.New(st.Since)
		}
	}
}

// StripBucket is the width of one strip bucket: the range divided by the
// number of buckets, rounded up to whole seconds.
func StripBucket(r *v1.TimeRange, buckets int32) time.Duration {
	span := r.GetEnd().AsTime().Sub(r.GetStart().AsTime())
	b := time.Duration(ceilDiv(span, time.Duration(max(buckets, 1))))
	return max(time.Duration(ceilDiv(b, time.Second))*time.Second, time.Second)
}

// Validate checks the registry: at least one system, each with a
// provider and a non-empty ID, and no ID twice.
func (s Systems) Validate() error {
	if len(s) == 0 {
		return errors.New("no system is configured")
	}
	seen := map[string]bool{}
	for i, sys := range s {
		id := sys.Ref.GetId()
		switch {
		case id == "":
			return fmt.Errorf("system %d has no ID", i+1)
		case seen[id]:
			return fmt.Errorf("system %q is listed twice", id)
		case sys.Provider == nil:
			return fmt.Errorf("system %q has no provider", id)
		}
		seen[id] = true
	}
	return nil
}
