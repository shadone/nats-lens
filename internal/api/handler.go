package api

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/iap"
)

// QueryTimeout bounds the time one call may spend in the provider.
const QueryTimeout = 30 * time.Second

// ErrUnauthenticated is returned for a write without a valid IAP assertion
// when the server is configured for IAP.
var ErrUnauthenticated = errors.New("unauthenticated")

// Handler implements LensService on top of a Provider.
type Handler struct {
	P   Provider
	Log *slog.Logger
	// IAP, when set, verifies the IAP assertion of every mute and unmute
	// call and records its email as the user. When nil, the IAP headers
	// are ignored and the request names the user.
	IAP *iap.Verifier
	// Version is the nats-lens version GetLensInfo reports.
	Version string
}

var _ natslensv1connect.LensServiceHandler = (*Handler)(nil)

// call normalizes the request, calls the provider within QueryTimeout and
// maps its errors to Connect codes. An unexpected error is logged and
// returned without its text, so that SQL and file paths do not reach the
// client.
func call[Req, Res any](ctx context.Context, h *Handler, procedure string, req *Req,
	normalize func(*Req) error, fn func(context.Context, *Req) (*Res, error),
) (*connect.Response[Res], error) {
	if normalize != nil {
		if err := normalize(req); err != nil {
			return nil, h.codeOf(procedure, err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	res, err := fn(ctx, req)
	if err != nil {
		return nil, h.codeOf(procedure, err)
	}
	h.withAccountNames(ctx, res)
	return connect.NewResponse(res), nil
}

func (h *Handler) codeOf(procedure string, err error) error {
	switch {
	case errors.Is(err, ErrInvalid):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, ErrFailedPrecondition):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, ErrUnauthenticated):
		return connect.NewError(connect.CodeUnauthenticated, err)
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	if h.Log != nil {
		h.Log.Error("api call failed", "procedure", procedure, "err", err)
	}
	return connect.NewError(connect.CodeInternal, errors.New("internal error; see the server log"))
}

// user returns the verified IAP user for a write, or "" without IAP.
func (h *Handler) user(ctx context.Context, header interface{ Get(string) string }) (string, error) {
	if h.IAP == nil {
		return "", nil
	}
	tok := header.Get(iap.Header)
	if tok == "" {
		return "", errors.Join(ErrUnauthenticated, errors.New("no IAP assertion"))
	}
	email, err := h.IAP.Verify(ctx, tok)
	if err != nil {
		if h.Log != nil {
			h.Log.Warn("IAP assertion rejected", "err", err)
		}
		return "", errors.Join(ErrUnauthenticated, errors.New("IAP assertion rejected"))
	}
	return email, nil
}

// withRange fills an unset range with the default span ending now.
func (h *Handler) withRange(r **v1.TimeRange) error {
	out, err := ResolveRange(*r, h.P.Now(), DefaultRange)
	if err == nil {
		*r = out
	}
	return err
}

// all returns the first error of checks run in order.
func all(checks ...func() error) error {
	for _, c := range checks {
		if err := c(); err != nil {
			return err
		}
	}
	return nil
}

func token(t string) func() error {
	return func() error { _, err := DecodeCursor(t); return err }
}

func (h *Handler) GetLensInfo(ctx context.Context, req *connect.Request[v1.GetLensInfoRequest]) (*connect.Response[v1.GetLensInfoResponse], error) {
	return call(ctx, h, "GetLensInfo", req.Msg, nil, func(ctx context.Context, _ *v1.GetLensInfoRequest) (*v1.GetLensInfoResponse, error) {
		res, err := h.P.Info(ctx)
		if err != nil {
			return nil, err
		}
		res.Version, res.IdentityMode = h.Version, v1.IdentityMode_IDENTITY_MODE_REQUEST
		if h.IAP != nil {
			res.IdentityMode = v1.IdentityMode_IDENTITY_MODE_IAP
		}
		return res, nil
	})
}

func (h *Handler) GetOverview(ctx context.Context, req *connect.Request[v1.GetOverviewRequest]) (*connect.Response[v1.GetOverviewResponse], error) {
	return call(ctx, h, "GetOverview", req.Msg, func(m *v1.GetOverviewRequest) (err error) {
		if err = h.withRange(&m.Range); err == nil {
			m.MaxPoints, err = points(m.GetMaxPoints(), DefaultOverviewPoint)
		}
		return err
	}, h.P.GetOverview)
}

func (h *Handler) ListFindings(ctx context.Context, req *connect.Request[v1.ListFindingsRequest]) (*connect.Response[v1.ListFindingsResponse], error) {
	return call(ctx, h, "ListFindings", req.Msg, func(m *v1.ListFindingsRequest) error {
		return all(
			func() error { return h.withRange(&m.Range) },
			func() (err error) { m.PageSize, err = pageSize(m.GetPageSize(), MaxFindingsPage); return err },
			token(m.GetPageToken()),
			func() error { return maxLen("states", len(m.GetStates()), MaxEnumFilters) },
			func() error { return maxLen("severities", len(m.GetSeverities()), MaxEnumFilters) },
			func() error { return checkEntity("entity", m.GetEntity(), true) },
		)
	}, h.P.ListFindings)
}

func (h *Handler) GetFinding(ctx context.Context, req *connect.Request[v1.GetFindingRequest]) (*connect.Response[v1.GetFindingResponse], error) {
	return call(ctx, h, "GetFinding", req.Msg, func(m *v1.GetFindingRequest) error {
		if m.GetId() == "" {
			return invalid("id is required")
		}
		return nil
	}, h.P.GetFinding)
}

func (h *Handler) MuteFinding(ctx context.Context, req *connect.Request[v1.MuteFindingRequest]) (*connect.Response[v1.MuteFindingResponse], error) {
	user, err := h.user(ctx, req.Header())
	if err != nil {
		return nil, h.codeOf("MuteFinding", err)
	}
	return call(ctx, h, "MuteFinding", req.Msg, func(m *v1.MuteFindingRequest) error {
		return normalizeMute(m, h.P.Now(), user)
	}, func(ctx context.Context, m *v1.MuteFindingRequest) (*v1.MuteFindingResponse, error) {
		mute, err := h.P.MuteFinding(ctx, m)
		return &v1.MuteFindingResponse{Mute: mute}, err
	})
}

func (h *Handler) UnmuteFinding(ctx context.Context, req *connect.Request[v1.UnmuteFindingRequest]) (*connect.Response[v1.UnmuteFindingResponse], error) {
	user, err := h.user(ctx, req.Header())
	if err != nil {
		return nil, h.codeOf("UnmuteFinding", err)
	}
	return call(ctx, h, "UnmuteFinding", req.Msg, func(m *v1.UnmuteFindingRequest) error {
		return normalizeUnmute(m, user)
	}, func(ctx context.Context, m *v1.UnmuteFindingRequest) (*v1.UnmuteFindingResponse, error) {
		mute, err := h.P.UnmuteFinding(ctx, m)
		return &v1.UnmuteFindingResponse{Mute: mute}, err
	})
}

func (h *Handler) ListMutes(ctx context.Context, req *connect.Request[v1.ListMutesRequest]) (*connect.Response[v1.ListMutesResponse], error) {
	return call(ctx, h, "ListMutes", req.Msg, func(m *v1.ListMutesRequest) (err error) {
		if m.PageSize, err = pageSize(m.GetPageSize(), MaxMutesPage); err == nil {
			err = token(m.GetPageToken())()
		}
		return err
	}, h.P.ListMutes)
}

func (h *Handler) GetStatusTimeline(ctx context.Context, req *connect.Request[v1.GetStatusTimelineRequest]) (*connect.Response[v1.GetStatusTimelineResponse], error) {
	return call(ctx, h, "GetStatusTimeline", req.Msg, func(m *v1.GetStatusTimelineRequest) error {
		return normalizeTimeline(m, h.P.Now())
	}, h.P.GetStatusTimeline)
}

func (h *Handler) ListEvents(ctx context.Context, req *connect.Request[v1.ListEventsRequest]) (*connect.Response[v1.ListEventsResponse], error) {
	return call(ctx, h, "ListEvents", req.Msg, func(m *v1.ListEventsRequest) error {
		return all(
			func() error { return h.withRange(&m.Range) },
			func() (err error) { m.PageSize, err = pageSize(m.GetPageSize(), MaxEventsPage); return err },
			token(m.GetPageToken()),
			func() error { return maxLen("sources", len(m.GetSources()), MaxEnumFilters) },
			func() error { return maxLen("kinds", len(m.GetKinds()), MaxKindFilters) },
			func() error { return checkEntity("entity", m.GetEntity(), true) },
		)
	}, h.P.ListEvents)
}

func (h *Handler) GetEvent(ctx context.Context, req *connect.Request[v1.GetEventRequest]) (*connect.Response[v1.GetEventResponse], error) {
	return call(ctx, h, "GetEvent", req.Msg, func(m *v1.GetEventRequest) error {
		if m.GetId() == "" {
			return invalid("id is required")
		}
		return nil
	}, h.P.GetEvent)
}

func (h *Handler) ListStreams(ctx context.Context, req *connect.Request[v1.ListStreamsRequest]) (*connect.Response[v1.ListStreamsResponse], error) {
	return call(ctx, h, "ListStreams", req.Msg, func(m *v1.ListStreamsRequest) error {
		return h.withRange(&m.Range)
	}, h.P.ListStreams)
}

func (h *Handler) GetStream(ctx context.Context, req *connect.Request[v1.GetStreamRequest]) (*connect.Response[v1.GetStreamResponse], error) {
	return call(ctx, h, "GetStream", req.Msg, func(m *v1.GetStreamRequest) error {
		if m.GetAccount() == "" || m.GetStream() == "" {
			return invalid("account and stream are required")
		}
		return h.withRange(&m.Range)
	}, h.P.GetStream)
}

func (h *Handler) ListConsumers(ctx context.Context, req *connect.Request[v1.ListConsumersRequest]) (*connect.Response[v1.ListConsumersResponse], error) {
	return call(ctx, h, "ListConsumers", req.Msg, func(m *v1.ListConsumersRequest) error {
		return h.withRange(&m.Range)
	}, h.P.ListConsumers)
}

func (h *Handler) GetConsumer(ctx context.Context, req *connect.Request[v1.GetConsumerRequest]) (*connect.Response[v1.GetConsumerResponse], error) {
	return call(ctx, h, "GetConsumer", req.Msg, func(m *v1.GetConsumerRequest) error {
		if m.GetAccount() == "" || m.GetStream() == "" || m.GetConsumer() == "" {
			return invalid("account, stream and consumer are required")
		}
		return h.withRange(&m.Range)
	}, h.P.GetConsumer)
}

func (h *Handler) ListServers(ctx context.Context, req *connect.Request[v1.ListServersRequest]) (*connect.Response[v1.ListServersResponse], error) {
	return call(ctx, h, "ListServers", req.Msg, func(m *v1.ListServersRequest) error {
		return h.withRange(&m.Range)
	}, h.P.ListServers)
}

func (h *Handler) GetServer(ctx context.Context, req *connect.Request[v1.GetServerRequest]) (*connect.Response[v1.GetServerResponse], error) {
	return call(ctx, h, "GetServer", req.Msg, func(m *v1.GetServerRequest) error {
		if m.GetName() == "" {
			return invalid("name is required")
		}
		return h.withRange(&m.Range)
	}, h.P.GetServer)
}

func (h *Handler) ListConnections(ctx context.Context, req *connect.Request[v1.ListConnectionsRequest]) (*connect.Response[v1.ListConnectionsResponse], error) {
	return call(ctx, h, "ListConnections", req.Msg, func(m *v1.ListConnectionsRequest) error {
		return all(
			func() error { return h.withRange(&m.Range) },
			func() (err error) { m.PageSize, err = pageSize(m.GetPageSize(), MaxConnectionsPage); return err },
			token(m.GetPageToken()),
		)
	}, h.P.ListConnections)
}

func (h *Handler) ListMetrics(context.Context, *connect.Request[v1.ListMetricsRequest]) (*connect.Response[v1.ListMetricsResponse], error) {
	return connect.NewResponse(&v1.ListMetricsResponse{Metrics: MetricList()}), nil
}

func (h *Handler) GetTimeSeries(ctx context.Context, req *connect.Request[v1.GetTimeSeriesRequest]) (*connect.Response[v1.GetTimeSeriesResponse], error) {
	return call(ctx, h, "GetTimeSeries", req.Msg, func(m *v1.GetTimeSeriesRequest) error {
		return normalizeTimeSeries(m, h.P.Now())
	}, h.P.GetTimeSeries)
}
