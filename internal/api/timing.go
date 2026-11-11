package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/timing"
)

// timingInterceptor records each API call's duration, its queries and the
// calls in flight, by procedure (timing.APIDuration and its siblings).
// Connect routes only the service's own procedures, so the label stays a
// closed set.
func timingInterceptor(systems Systems) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			proc := procedureName(req.Spec().Procedure)
			sys := callSystem(systems, req.Any())
			inFlight := timing.APIInFlight.With(sys, proc)
			inFlight.Add(1)
			defer inFlight.Add(-1)
			call := &timing.Call{Procedure: proc}
			ctx = admit.WithFlow(timing.WithCall(timing.WithSource(ctx, timing.SourceAPI), call))
			start := time.Now()
			res, err := next(ctx, req)
			err = overloadCode(admit.FlowOf(ctx), err)
			code := "ok"
			if err != nil {
				code = connect.CodeOf(err).String()
			}
			timing.Since(timing.APIDuration.With(sys, proc, code), start)
			timing.APIQueries.With(sys, proc).Observe(float64(call.Queries()))
			timing.APIQuerySeconds.With(sys, proc).Observe(call.QuerySeconds())
			return res, err
		}
	}
}

// overloadCode answers resource_exhausted for a call that ran out of time
// after one of its queries was refused for the store's query budget: the
// provider may return the context's deadline rather than the refusal.
// Any other failure keeps its code, so that a real error is not reported
// as a busy system.
func overloadCode(f *admit.Flow, err error) error {
	if err == nil || !f.Overloaded() {
		return err
	}
	if connect.CodeOf(err) == connect.CodeDeadlineExceeded && errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeResourceExhausted, errBusy)
	}
	return err
}

// errBusy is the message of a call refused for the query budget.
var errBusy = errors.New("the system is busy: its query budget stayed in use until the call's deadline; try again shortly")

// callSystem is the system label of a request: the configured system it
// names, the default for an empty name, timing.UnknownSystem for a name no
// system has and timing.AllSystems for a request without a system field.
func callSystem(systems Systems, msg any) string {
	r, ok := msg.(interface{ GetSystem() string })
	if !ok {
		return timing.AllSystems
	}
	s, err := systems.Resolve(r.GetSystem())
	if err != nil {
		return timing.UnknownSystem
	}
	return s.Ref.GetId()
}

// procedureName is the method of a Connect procedure path,
// "/natslens.v1.LensService/GetOverview" -> "GetOverview".
func procedureName(p string) string {
	return p[strings.LastIndexByte(p, '/')+1:]
}

// addTimings appends the process's timing families to f.
func (f *families) addTimings() {
	for _, fam := range timing.Families() {
		f.declare(fam.Name, fam.Type, fam.Help)
		for _, l := range fam.Lines {
			f.add(fam.Name, l)
		}
	}
}
