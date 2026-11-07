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
	CollectionStoreFailed      CollectionState = "store_failed"
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
	live := m.GetRange().GetEnd() == nil
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
	key := summaryKey(m, live)
	res := &v1.ListSystemsResponse{Range: m.GetRange(), DefaultSystem: d.Ref.GetId(), Systems: make([]*v1.SystemSummary, len(h.Systems))}
	var wg sync.WaitGroup
	for i, sys := range h.Systems {
		wg.Go(func() {
			// Each provider reads its own copy of the request.
			sum := h.summary(ctx, sys, proto.CloneOf(m), key, timeout)
			sum.System = proto.CloneOf(sys.Ref)
			res.Systems[i] = sum
		})
	}
	wg.Wait()
	return connect.NewResponse(res), nil
}

// summary reads one system's summary within the timeout, with the
// runtime's collection status applied. Calls for the same range while a
// run is in flight share that run and its result. A run whose provider
// outlives the timeout is not started again: until it returns, a call
// gets the timeout card at once and starts nothing.
func (h *Handler) summary(ctx context.Context, sys System, req *v1.ListSystemsRequest, key string, timeout time.Duration) *v1.SystemSummary {
	tooLong := &v1.SystemSummary{Error: fmt.Sprintf("the summary took longer than %s", timeout)}
	f := h.summaryFlights.join(sys.Ref.GetId(), key, timeout, func(f *summaryFlight) {
		var runCtx context.Context
		runCtx, f.cancel = context.WithTimeout(context.WithoutCancel(ctx), timeout)
		h.runSummary(runCtx, sys, req, f)
	})
	if f == nil {
		return tooLong
	}
	wait := time.NewTimer(time.Until(f.started.Add(timeout)))
	defer wait.Stop()
	var r summaryResult
	select {
	case <-f.ready:
		r = f.res
	case <-ctx.Done():
		h.summaryFlights.leave(f)
		r.err = context.Canceled
	case <-wait.C:
		r.err = context.DeadlineExceeded
	}
	sum := summaryCard(proto.CloneOf(r.sum), r.err, tooLong)
	if r.status != nil && !errors.Is(r.err, context.Canceled) {
		withCollectionStatus(sum, *r.status)
	}
	return sum
}

// summaryCard is the card for a provider's result; the run has logged
// its failure.
func summaryCard(sum *v1.SystemSummary, err error, tooLong *v1.SystemSummary) *v1.SystemSummary {
	switch {
	case errors.Is(err, context.Canceled):
		// The caller went away; nothing failed.
		return &v1.SystemSummary{Error: "the request was canceled"}
	case errors.Is(err, context.DeadlineExceeded):
		return tooLong
	case err != nil:
		return &v1.SystemSummary{Error: "the summary could not be read; see the server log"}
	case sum == nil:
		return &v1.SystemSummary{}
	}
	return sum
}

func (h *Handler) warnSummary(sys System, err error) {
	if h.Log != nil {
		h.Log.Warn("system summary failed", "system", sys.Ref.GetId(), "err", err)
	}
}

// collectionStates maps the runtime's states to the API's.
var collectionStates = map[CollectionState]v1.CollectionState{
	CollectionStoreFailed:      v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED,
	CollectionStarting:         v1.CollectionState_COLLECTION_STATE_STARTING,
	CollectionCollecting:       v1.CollectionState_COLLECTION_STATE_COLLECTING,
	CollectionNotConnected:     v1.CollectionState_COLLECTION_STATE_NOT_CONNECTED,
	CollectionConnectionClosed: v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED,
}

// withCollectionStatus sets the summary's collection fields and its
// freshness's collecting from the runtime's status. The error and its time
// are kept only while the collector is not collecting.
func withCollectionStatus(sum *v1.SystemSummary, st CollectionStatus) {
	if st.State == CollectionStoreFailed {
		sum.Error = st.LastError
	}
	sum.Collection = collectionStates[st.State]
	sum.CollectionError, sum.CollectionErrorSince = "", nil
	if st.notCollecting() {
		sum.CollectionError = st.LastError
		if !st.Since.IsZero() {
			sum.CollectionErrorSince = timestamppb.New(st.Since)
		}
	}
	withCollecting(sum.GetFreshness(), st)
}

// notCollecting reports whether the collector's connection is not
// connected or closed for good.
func (st CollectionStatus) notCollecting() bool {
	return st.State == CollectionStoreFailed || st.State == CollectionNotConnected || st.State == CollectionConnectionClosed
}

// withCollecting sets the freshness's collecting and the time it stopped
// from the runtime's status; f may be nil.
func withCollecting(f *v1.Freshness, st CollectionStatus) {
	if f == nil {
		return
	}
	f.Collecting, f.NotCollectingSince = !st.notCollecting(), nil
	if st.notCollecting() && !st.Since.IsZero() {
		f.NotCollectingSince = timestamppb.New(st.Since)
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
