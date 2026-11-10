package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/iap"
)

// QueryTimeout bounds the time one call may spend in the provider.
const QueryTimeout = 30 * time.Second

// ErrUnauthenticated is returned for a write without a valid IAP assertion
// when the server is configured for IAP.
var ErrUnauthenticated = errors.New("unauthenticated")

// Handler implements LensService on top of one Provider per system.
type Handler struct {
	// Systems are the systems served; the first is the default.
	Systems Systems
	Log     *slog.Logger
	// IAP, when set, verifies the IAP assertion of every write (mute,
	// unmute, acknowledge, end acknowledgement) and records its email as
	// the user. When nil, the IAP headers are ignored and the request
	// names the user.
	IAP *iap.Verifier
	// Version is the nats-lens version GetLensInfo reports.
	Version string
	// MetricsTimeout bounds each system's part of a /metrics scrape; zero
	// means DefaultMetricsTimeout.
	MetricsTimeout time.Duration
	// SummaryTimeout bounds each system's summary in ListSystems; zero
	// means DefaultSummaryTimeout.
	SummaryTimeout time.Duration
	// ShellTimeout bounds the system's part of GetShellStatus; zero means
	// DefaultShellTimeout.
	ShellTimeout time.Duration

	// summaryFlights are the ListSystems summaries in flight.
	summaryFlights summaryFlights
	// badgeCache keeps GetShellStatus's badge summaries.
	badgeCache badgeCache
	// badgeWait replaces badgeWait in tests; zero means badgeWait.
	badgeWait time.Duration
}

var _ natslensv1connect.LensServiceHandler = (*Handler)(nil)

// systemRequest is a request that names its system.
type systemRequest interface{ GetSystem() string }

// call resolves the request's system, normalizes the request, calls the
// system's provider within QueryTimeout and maps its errors to Connect
// codes. An unknown system is invalid_argument, and no provider is
// called. An unexpected error is logged and returned without its text, so
// that SQL and file paths do not reach the client.
func call[Req systemRequest, Res any](ctx context.Context, h *Handler, procedure string, req Req,
	normalize func(Provider, Req) error, fn func(System, context.Context, Req) (*Res, error),
) (*connect.Response[Res], error) {
	return callWithin(ctx, h, procedure, QueryTimeout, req, normalize, fn)
}

// callWithin is call with a time limit other than QueryTimeout.
func callWithin[Req systemRequest, Res any](ctx context.Context, h *Handler, procedure string, timeout time.Duration, req Req,
	normalize func(Provider, Req) error, fn func(System, context.Context, Req) (*Res, error),
) (*connect.Response[Res], error) {
	sys, err := h.Systems.Resolve(req.GetSystem())
	if err != nil {
		return nil, h.codeOf(procedure, err)
	}
	if normalize != nil {
		if err := normalize(sys.Provider, req); err != nil {
			return nil, h.codeOf(procedure, err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := fn(sys, ctx, req)
	if err != nil {
		return nil, h.codeOf(procedure, err)
	}
	if err := h.withEntityPageAccountNames(ctx, sys.Provider, res); err != nil {
		return nil, h.codeOf(procedure, err)
	}
	// Account decoration happens after provider paging; enforce the final wire
	// budget too, including names supplied by the collector.
	switch m := any(res).(type) {
	case *v1.ListStreamsResponse:
		if !fitsEntityBytes(m, MaxEntityPageBytes) {
			return nil, h.codeOf(procedure, fmt.Errorf("%w: decorated stream page exceeds byte budget; request a smaller limit", ErrResourceExhausted))
		}
	case *v1.ListConsumersResponse:
		if !fitsEntityBytes(m, MaxEntityPageBytes) {
			return nil, h.codeOf(procedure, fmt.Errorf("%w: decorated consumer page exceeds byte budget; request a smaller limit", ErrResourceExhausted))
		}
	}
	return connect.NewResponse(res), nil
}

// on adapts a Provider method expression, such as Provider.ListServers,
// to call's signature.
func on[Req, Res any](f func(Provider, context.Context, Req) (*Res, error)) func(System, context.Context, Req) (*Res, error) {
	return func(s System, ctx context.Context, r Req) (*Res, error) { return f(s.Provider, ctx, r) }
}

func (h *Handler) codeOf(procedure string, err error) error {
	switch {
	case errors.Is(err, ErrResourceExhausted):
		return connect.NewError(connect.CodeResourceExhausted, err)
	case errors.Is(err, ErrInvalid):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, ErrFailedPrecondition):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, ErrAlreadyExists):
		return connect.NewError(connect.CodeAlreadyExists, err)
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

// rangeReference allows captured data to include a zero-duration latest sample
// in a derived half-open interval without changing its observed reference time.
func rangeReference(p Provider) time.Time {
	if p, ok := p.(interface{ RangeReference() time.Time }); ok {
		return p.RangeReference()
	}
	return p.Now()
}

// withRange fills an unset range with the default span ending at the
// provider's now.
func withRange(p Provider, r **v1.TimeRange) error {
	out, err := ResolveRange(*r, rangeReference(p), DefaultRange)
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
	return call(ctx, h, "GetLensInfo", req.Msg, nil, func(s System, ctx context.Context, _ *v1.GetLensInfoRequest) (*v1.GetLensInfoResponse, error) {
		res, err := s.Provider.Info(ctx)
		if err != nil {
			return nil, err
		}
		res.System, res.SystemName = s.Ref.GetId(), s.Ref.GetName()
		res.Now = timestamppb.New(s.Provider.Now())
		res.Version, res.IdentityMode = h.Version, v1.IdentityMode_IDENTITY_MODE_REQUEST
		if h.IAP != nil {
			res.IdentityMode = v1.IdentityMode_IDENTITY_MODE_IAP
		}
		return res, nil
	})
}

func (h *Handler) GetOverview(ctx context.Context, req *connect.Request[v1.GetOverviewRequest]) (*connect.Response[v1.GetOverviewResponse], error) {
	return call(ctx, h, "GetOverview", req.Msg, func(p Provider, m *v1.GetOverviewRequest) (err error) {
		if err = withRange(p, &m.Range); err == nil {
			m.MaxPoints, err = points(m.GetMaxPoints(), DefaultOverviewPoint)
		}
		return err
	}, func(s System, ctx context.Context, m *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
		res, err := s.Provider.GetOverview(ctx, m)
		if err == nil && s.Status != nil {
			withCollecting(res.GetFreshness(), s.Status())
		}
		return res, err
	})
}

func (h *Handler) ListFindings(ctx context.Context, req *connect.Request[v1.ListFindingsRequest]) (*connect.Response[v1.ListFindingsResponse], error) {
	return call(ctx, h, "ListFindings", req.Msg, func(p Provider, m *v1.ListFindingsRequest) error {
		return all(
			func() error { return withRange(p, &m.Range) },
			func() (err error) { m.PageSize, err = pageSize(m.GetPageSize(), MaxFindingsPage); return err },
			token(m.GetPageToken()),
			func() error { return maxLen("states", len(m.GetStates()), MaxEnumFilters) },
			func() error { return maxLen("severities", len(m.GetSeverities()), MaxEnumFilters) },
			func() error { return checkEntity("entity", m.GetEntity(), true) },
			func() error { return searchText(&m.Text) },
			func() error { return checkAccountFilter(m.GetAccount()) },
		)
	}, on(Provider.ListFindings))
}

func (h *Handler) GetFinding(ctx context.Context, req *connect.Request[v1.GetFindingRequest]) (*connect.Response[v1.GetFindingResponse], error) {
	return call(ctx, h, "GetFinding", req.Msg, func(_ Provider, m *v1.GetFindingRequest) error {
		if m.GetId() == "" {
			return invalid("id is required")
		}
		return nil
	}, on(Provider.GetFinding))
}

func (h *Handler) MuteFinding(ctx context.Context, req *connect.Request[v1.MuteFindingRequest]) (*connect.Response[v1.MuteFindingResponse], error) {
	user, err := h.user(ctx, req.Header())
	if err != nil {
		return nil, h.codeOf("MuteFinding", err)
	}
	return call(ctx, h, "MuteFinding", req.Msg, func(p Provider, m *v1.MuteFindingRequest) error {
		return normalizeMute(m, p.Now(), user)
	}, func(s System, ctx context.Context, m *v1.MuteFindingRequest) (*v1.MuteFindingResponse, error) {
		mute, err := s.Provider.MuteFinding(ctx, m)
		return &v1.MuteFindingResponse{Mute: mute}, err
	})
}

func (h *Handler) UnmuteFinding(ctx context.Context, req *connect.Request[v1.UnmuteFindingRequest]) (*connect.Response[v1.UnmuteFindingResponse], error) {
	user, err := h.user(ctx, req.Header())
	if err != nil {
		return nil, h.codeOf("UnmuteFinding", err)
	}
	return call(ctx, h, "UnmuteFinding", req.Msg, func(_ Provider, m *v1.UnmuteFindingRequest) error {
		return normalizeUnmute(m, user)
	}, func(s System, ctx context.Context, m *v1.UnmuteFindingRequest) (*v1.UnmuteFindingResponse, error) {
		mute, err := s.Provider.UnmuteFinding(ctx, m)
		return &v1.UnmuteFindingResponse{Mute: mute}, err
	})
}

func (h *Handler) AcknowledgeFinding(ctx context.Context, req *connect.Request[v1.AcknowledgeFindingRequest]) (*connect.Response[v1.AcknowledgeFindingResponse], error) {
	user, err := h.user(ctx, req.Header())
	if err != nil {
		return nil, h.codeOf("AcknowledgeFinding", err)
	}
	return call(ctx, h, "AcknowledgeFinding", req.Msg, func(_ Provider, m *v1.AcknowledgeFindingRequest) error {
		return normalizeAck(m, user)
	}, func(s System, ctx context.Context, m *v1.AcknowledgeFindingRequest) (*v1.AcknowledgeFindingResponse, error) {
		a, err := s.Provider.AcknowledgeFinding(ctx, m)
		return &v1.AcknowledgeFindingResponse{Acknowledgement: a}, err
	})
}

func (h *Handler) EndAcknowledgement(ctx context.Context, req *connect.Request[v1.EndAcknowledgementRequest]) (*connect.Response[v1.EndAcknowledgementResponse], error) {
	user, err := h.user(ctx, req.Header())
	if err != nil {
		return nil, h.codeOf("EndAcknowledgement", err)
	}
	return call(ctx, h, "EndAcknowledgement", req.Msg, func(_ Provider, m *v1.EndAcknowledgementRequest) error {
		return normalizeEndAck(m, user)
	}, func(s System, ctx context.Context, m *v1.EndAcknowledgementRequest) (*v1.EndAcknowledgementResponse, error) {
		a, err := s.Provider.EndAcknowledgement(ctx, m)
		return &v1.EndAcknowledgementResponse{Acknowledgement: a}, err
	})
}

func (h *Handler) ListMutes(ctx context.Context, req *connect.Request[v1.ListMutesRequest]) (*connect.Response[v1.ListMutesResponse], error) {
	return call(ctx, h, "ListMutes", req.Msg, func(_ Provider, m *v1.ListMutesRequest) (err error) {
		if m.PageSize, err = pageSize(m.GetPageSize(), MaxMutesPage); err == nil {
			err = token(m.GetPageToken())()
		}
		return err
	}, on(Provider.ListMutes))
}

func (h *Handler) AcceptFindings(ctx context.Context, req *connect.Request[v1.AcceptFindingsRequest]) (*connect.Response[v1.AcceptFindingsResponse], error) {
	user, err := h.user(ctx, req.Header())
	if err != nil {
		return nil, h.codeOf("AcceptFindings", err)
	}
	return call(ctx, h, "AcceptFindings", req.Msg, func(p Provider, m *v1.AcceptFindingsRequest) error {
		return normalizeAcceptMany(m, p.Now(), user)
	}, func(s System, ctx context.Context, m *v1.AcceptFindingsRequest) (*v1.AcceptFindingsResponse, error) {
		ms, err := s.Provider.AcceptFindings(ctx, m)
		return &v1.AcceptFindingsResponse{Mutes: ms}, err
	})
}

func (h *Handler) ListAcceptances(ctx context.Context, req *connect.Request[v1.ListAcceptancesRequest]) (*connect.Response[v1.ListAcceptancesResponse], error) {
	return call(ctx, h, "ListAcceptances", req.Msg, func(_ Provider, m *v1.ListAcceptancesRequest) (err error) {
		if m.PageSize, err = pageSize(m.GetPageSize(), MaxMutesPage); err == nil {
			if err = token(m.GetPageToken())(); err == nil {
				err = checkAccountFilter(m.GetAccount())
			}
		}
		return err
	}, on(Provider.ListAcceptances))
}

// ASVS 1.5.2, 2.2.1/2.2.2: generated typed input uses the existing trusted range validator.
func (h *Handler) GetTimeWindows(ctx context.Context, req *connect.Request[v1.GetTimeWindowsRequest]) (*connect.Response[v1.GetTimeWindowsResponse], error) {
	return call(ctx, h, "GetTimeWindows", req.Msg, func(p Provider, m *v1.GetTimeWindowsRequest) error { return withRange(p, &m.Range) }, on(Provider.GetTimeWindows))
}

func (h *Handler) GetStatusTimeline(ctx context.Context, req *connect.Request[v1.GetStatusTimelineRequest]) (*connect.Response[v1.GetStatusTimelineResponse], error) {
	return call(ctx, h, "GetStatusTimeline", req.Msg, func(p Provider, m *v1.GetStatusTimelineRequest) error {
		return normalizeTimeline(m, rangeReference(p))
	}, on(Provider.GetStatusTimeline))
}

func (h *Handler) ListEvents(ctx context.Context, req *connect.Request[v1.ListEventsRequest]) (*connect.Response[v1.ListEventsResponse], error) {
	return call(ctx, h, "ListEvents", req.Msg, func(p Provider, m *v1.ListEventsRequest) error {
		return all(
			func() error { return withRange(p, &m.Range) },
			func() (err error) { m.PageSize, err = pageSize(m.GetPageSize(), MaxEventsPage); return err },
			token(m.GetPageToken()),
			func() error { return maxLen("sources", len(m.GetSources()), MaxEnumFilters) },
			func() error { return maxLen("kinds", len(m.GetKinds()), MaxKindFilters) },
			func() error { return histogramBuckets(m.GetHistogramBuckets()) },
			func() error { _, err := EventSelection(m.SelectedIds, m.EventReadId); return err },
			func() error { return checkEntity("entity", m.GetEntity(), true) },
			func() error { return searchText(&m.Text) },
		)
	}, on(Provider.ListEvents))
}

func (h *Handler) GetEvent(ctx context.Context, req *connect.Request[v1.GetEventRequest]) (*connect.Response[v1.GetEventResponse], error) {
	return call(ctx, h, "GetEvent", req.Msg, func(_ Provider, m *v1.GetEventRequest) error {
		if m.GetId() == "" {
			return invalid("id is required")
		}
		_, err := EventSelection(nil, m.EventReadId)
		return err
	}, on(Provider.GetEvent))
}

func (h *Handler) ListStreams(ctx context.Context, req *connect.Request[v1.ListStreamsRequest]) (*connect.Response[v1.ListStreamsResponse], error) {
	return call(ctx, h, "ListStreams", req.Msg, func(p Provider, m *v1.ListStreamsRequest) error {
		var err error
		if _, err = EntityLimit(m.GetLimit()); err != nil {
			return err
		}
		m.Range, err = EntityListRange(m.GetRange(), m.GetPageToken(), rangeReference(p))
		return err
	}, on(Provider.ListStreams))
}

func (h *Handler) GetStream(ctx context.Context, req *connect.Request[v1.GetStreamRequest]) (*connect.Response[v1.GetStreamResponse], error) {
	return call(ctx, h, "GetStream", req.Msg, func(p Provider, m *v1.GetStreamRequest) error {
		if m.GetAccount() == "" || m.GetStream() == "" {
			return invalid("account and stream are required")
		}
		return withRange(p, &m.Range)
	}, on(Provider.GetStream))
}

func (h *Handler) ListConsumers(ctx context.Context, req *connect.Request[v1.ListConsumersRequest]) (*connect.Response[v1.ListConsumersResponse], error) {
	return call(ctx, h, "ListConsumers", req.Msg, func(p Provider, m *v1.ListConsumersRequest) error {
		var err error
		if _, err = EntityLimit(m.GetLimit()); err != nil {
			return err
		}
		m.Range, err = EntityListRange(m.GetRange(), m.GetPageToken(), rangeReference(p))
		return err
	}, on(Provider.ListConsumers))
}

func (h *Handler) GetConsumer(ctx context.Context, req *connect.Request[v1.GetConsumerRequest]) (*connect.Response[v1.GetConsumerResponse], error) {
	return call(ctx, h, "GetConsumer", req.Msg, func(p Provider, m *v1.GetConsumerRequest) error {
		if m.GetAccount() == "" || m.GetStream() == "" || m.GetConsumer() == "" {
			return invalid("account, stream and consumer are required")
		}
		return withRange(p, &m.Range)
	}, on(Provider.GetConsumer))
}

func (h *Handler) GetPlacement(ctx context.Context, req *connect.Request[v1.GetPlacementRequest]) (*connect.Response[v1.GetPlacementResponse], error) {
	return call(ctx, h, "GetPlacement", req.Msg, func(p Provider, m *v1.GetPlacementRequest) error {
		if _, ok := v1.PlacementFilter_name[int32(m.GetFilter())]; !ok {
			return invalid("unknown filter %d", m.GetFilter())
		}
		// ASVS 2.2.1: bound and validate the selected-cell key at the service boundary.
		if m.GetSelectedAsset() != "" || m.GetSelectedServer() != "" {
			parts := strings.Split(m.GetSelectedAsset(), "/")
			if len(m.GetSelectedAsset()) > 1024 || len(m.GetSelectedServer()) > 256 || m.GetSelectedServer() == "" || (len(parts) != 2 && len(parts) != 3) {
				return invalid("selected asset and server are required; asset is account/stream[/consumer]")
			}
			for _, part := range parts {
				if part == "" {
					return invalid("selected asset has an empty component")
				}
			}
		}
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error) {
		res, err := s.Provider.GetPlacement(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishPlacement(res, m.GetFilter()), nil
	})
}

func (h *Handler) ListAssetPlacement(ctx context.Context, req *connect.Request[v1.ListAssetPlacementRequest]) (*connect.Response[v1.ListAssetPlacementResponse], error) {
	return call(ctx, h, "ListAssetPlacement", req.Msg, func(p Provider, m *v1.ListAssetPlacementRequest) error {
		return all(
			func() error { return withRange(p, &m.Range) },
			func() error { return checkAssetPlacement(m) },
		)
	}, on(Provider.ListAssetPlacement))
}

func (h *Handler) ListServers(ctx context.Context, req *connect.Request[v1.ListServersRequest]) (*connect.Response[v1.ListServersResponse], error) {
	return call(ctx, h, "ListServers", req.Msg, func(p Provider, m *v1.ListServersRequest) error {
		return withRange(p, &m.Range)
	}, on(Provider.ListServers))
}

func (h *Handler) GetServer(ctx context.Context, req *connect.Request[v1.GetServerRequest]) (*connect.Response[v1.GetServerResponse], error) {
	return call(ctx, h, "GetServer", req.Msg, func(p Provider, m *v1.GetServerRequest) error {
		if m.GetName() == "" {
			return invalid("name is required")
		}
		return withRange(p, &m.Range)
	}, on(Provider.GetServer))
}

func (h *Handler) ListConnections(ctx context.Context, req *connect.Request[v1.ListConnectionsRequest]) (*connect.Response[v1.ListConnectionsResponse], error) {
	return call(ctx, h, "ListConnections", req.Msg, func(p Provider, m *v1.ListConnectionsRequest) error {
		return all(
			func() error { return withRange(p, &m.Range) },
			func() (err error) { m.PageSize, err = pageSize(m.GetPageSize(), MaxConnectionsPage); return err },
			token(m.GetPageToken()),
		)
	}, on(Provider.ListConnections))
}

func (h *Handler) ListMetrics(context.Context, *connect.Request[v1.ListMetricsRequest]) (*connect.Response[v1.ListMetricsResponse], error) {
	return connect.NewResponse(&v1.ListMetricsResponse{Metrics: MetricList()}), nil
}

func (h *Handler) GetTimeSeries(ctx context.Context, req *connect.Request[v1.GetTimeSeriesRequest]) (*connect.Response[v1.GetTimeSeriesResponse], error) {
	return call(ctx, h, "GetTimeSeries", req.Msg, func(p Provider, m *v1.GetTimeSeriesRequest) error {
		return normalizeTimeSeries(m, rangeReference(p))
	}, on(Provider.GetTimeSeries))
}

func (h *Handler) ListAccounts(ctx context.Context, req *connect.Request[v1.ListAccountsRequest]) (*connect.Response[v1.ListAccountsResponse], error) {
	return call(ctx, h, "ListAccounts", req.Msg, func(p Provider, m *v1.ListAccountsRequest) error {
		return withRange(p, &m.Range)
	}, on(Provider.ListAccounts))
}

func (h *Handler) ListKeyValueStores(ctx context.Context, req *connect.Request[v1.ListKeyValueStoresRequest]) (*connect.Response[v1.ListKeyValueStoresResponse], error) {
	return call(ctx, h, "ListKeyValueStores", req.Msg, func(p Provider, m *v1.ListKeyValueStoresRequest) error {
		return withRange(p, &m.Range)
	}, on(Provider.ListKeyValueStores))
}

func (h *Handler) ListObjectStores(ctx context.Context, req *connect.Request[v1.ListObjectStoresRequest]) (*connect.Response[v1.ListObjectStoresResponse], error) {
	return call(ctx, h, "ListObjectStores", req.Msg, func(p Provider, m *v1.ListObjectStoresRequest) error {
		return withRange(p, &m.Range)
	}, on(Provider.ListObjectStores))
}

func (h *Handler) GetReplay(ctx context.Context, req *connect.Request[v1.GetReplayRequest]) (*connect.Response[v1.GetReplayResponse], error) {
	return call(ctx, h, "GetReplay", req.Msg, func(p Provider, m *v1.GetReplayRequest) error {
		return normalizeReplay(m, rangeReference(p))
	}, func(s System, ctx context.Context, m *v1.GetReplayRequest) (*v1.GetReplayResponse, error) {
		f, err := s.Provider.Replay(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishReplay(f), nil
	})
}

func (h *Handler) GetTopology(ctx context.Context, req *connect.Request[v1.GetTopologyRequest]) (*connect.Response[v1.GetTopologyResponse], error) {
	return call(ctx, h, "GetTopology", req.Msg, func(p Provider, m *v1.GetTopologyRequest) error {
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetTopologyRequest) (*v1.GetTopologyResponse, error) {
		f, err := s.Provider.Topology(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishTopology(f), nil
	})
}

func (h *Handler) GetAccountsAndAuth(ctx context.Context, req *connect.Request[v1.GetAccountsAndAuthRequest]) (*connect.Response[v1.GetAccountsAndAuthResponse], error) {
	return call(ctx, h, "GetAccountsAndAuth", req.Msg, func(p Provider, m *v1.GetAccountsAndAuthRequest) error {
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetAccountsAndAuthRequest) (*v1.GetAccountsAndAuthResponse, error) {
		f, err := s.Provider.AccountsAndAuth(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishAccounts(f), nil
	})
}

func (h *Handler) GetCapacity(ctx context.Context, req *connect.Request[v1.GetCapacityRequest]) (*connect.Response[v1.GetCapacityResponse], error) {
	return call(ctx, h, "GetCapacity", req.Msg, func(p Provider, m *v1.GetCapacityRequest) error {
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetCapacityRequest) (*v1.GetCapacityResponse, error) {
		f, err := s.Provider.Capacity(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishCapacity(f), nil
	})
}

func (h *Handler) GetSubjects(ctx context.Context, req *connect.Request[v1.GetSubjectsRequest]) (*connect.Response[v1.GetSubjectsResponse], error) {
	return call(ctx, h, "GetSubjects", req.Msg, func(p Provider, m *v1.GetSubjectsRequest) error {
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetSubjectsRequest) (*v1.GetSubjectsResponse, error) {
		f, err := s.Provider.Subjects(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishSubjects(f, m.GetAccount(), m.GetFilter()), nil
	})
}

func (h *Handler) GetMicroServices(ctx context.Context, req *connect.Request[v1.GetMicroServicesRequest]) (*connect.Response[v1.GetMicroServicesResponse], error) {
	return call(ctx, h, "GetMicroServices", req.Msg, func(p Provider, m *v1.GetMicroServicesRequest) error {
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetMicroServicesRequest) (*v1.GetMicroServicesResponse, error) {
		f, err := s.Provider.MicroServices(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishMicro(f, m.GetAccount()), nil
	})
}

func (h *Handler) GetApplications(ctx context.Context, req *connect.Request[v1.GetApplicationsRequest]) (*connect.Response[v1.GetApplicationsResponse], error) {
	return call(ctx, h, "GetApplications", req.Msg, func(p Provider, m *v1.GetApplicationsRequest) error {
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetApplicationsRequest) (*v1.GetApplicationsResponse, error) {
		f, err := s.Provider.Applications(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishApplications(f, m.GetAccount(), m.GetIncludeTooling()), nil
	})
}

func (h *Handler) MatchSubject(ctx context.Context, req *connect.Request[v1.MatchSubjectRequest]) (*connect.Response[v1.MatchSubjectResponse], error) {
	return call(ctx, h, "MatchSubject", req.Msg, func(p Provider, m *v1.MatchSubjectRequest) error {
		if err := ValidateLiteralSubject(m.GetSubject()); err != nil {
			return err
		}
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.MatchSubjectRequest) (*v1.MatchSubjectResponse, error) {
		f, err := s.Provider.MatchSubject(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishMatch(f, m.GetAccount(), m.GetSubject()), nil
	})
}

func (h *Handler) GetDataHistory(ctx context.Context, req *connect.Request[v1.GetDataHistoryRequest]) (*connect.Response[v1.GetDataHistoryResponse], error) {
	return call(ctx, h, "GetDataHistory", req.Msg, func(p Provider, m *v1.GetDataHistoryRequest) error {
		return withRange(p, &m.Range)
	}, func(s System, ctx context.Context, m *v1.GetDataHistoryRequest) (*v1.GetDataHistoryResponse, error) {
		f, err := s.Provider.DataHistory(ctx, m)
		if err != nil {
			return nil, err
		}
		return FinishDataHistory(f), nil
	})
}

// GetOnCall serves one read-only card within the usual query deadline.
func (h *Handler) GetOnCall(ctx context.Context, req *connect.Request[v1.GetOnCallRequest]) (*connect.Response[v1.GetOnCallResponse], error) {
	return call(ctx, h, "GetOnCall", req.Msg, func(_ Provider, m *v1.GetOnCallRequest) error {
		// ASVS 2.2.1: bound the lookup key at the service boundary.
		if m.GetFindingId() == "" {
			return invalid("finding_id is required")
		}
		return maxLen("finding_id", len(m.GetFindingId()), 4096)
	}, func(s System, ctx context.Context, m *v1.GetOnCallRequest) (*v1.GetOnCallResponse, error) {
		f, err := s.Provider.OnCall(ctx, m)
		if err != nil {
			return nil, err
		}
		a, b := OnCallSpan(f.Finding, f.At)
		facts, err := s.Provider.Replay(ctx, &v1.GetReplayRequest{A: timestamppb.New(a), B: timestamppb.New(b)})
		if errors.Is(err, ErrInvalid) {
			return FinishOnCall(f, nil, err.Error()), nil
		}
		if err != nil {
			return nil, err
		}
		replay := FinishReplay(facts)
		if facts.HistoryStart.IsZero() || a.Before(facts.HistoryStart) {
			return FinishOnCall(f, nil, "The story starts before stored history."), nil
		}
		return FinishOnCall(f, replay, ""), nil
	})
}

func (h *Handler) GetStorageChange(ctx context.Context, req *connect.Request[v1.GetStorageChangeRequest]) (*connect.Response[v1.GetStorageChangeResponse], error) {
	return call(ctx, h, "GetStorageChange", req.Msg, func(p Provider, m *v1.GetStorageChangeRequest) error {
		if err := validateStorageChangeSelection(m); err != nil {
			return err
		}
		if m.A == nil || m.B == nil || m.A.CheckValid() != nil || m.B.CheckValid() != nil || !m.A.AsTime().Before(m.B.AsTime()) || m.B.AsTime().After(rangeReference(p)) {
			return invalid("valid ordered nonfuture a and b are required")
		}
		if m.B.AsTime().Sub(m.A.AsTime()) > MaxStorageChangeSpan {
			return invalid("storage comparison is at most 30 days")
		}
		if m.EpochA < 0 || m.EpochB < 0 || m.PageSize < 0 || m.PageSize > MaxStorageChangePage || len(m.PageToken) > 1024 || len(m.Account) > 1024 {
			return invalid("invalid storage comparison scope, pins or page budget")
		}
		if m.PageSize == 0 {
			m.PageSize = MaxStorageChangePage
		}
		return nil
	}, func(s System, ctx context.Context, m *v1.GetStorageChangeRequest) (*v1.GetStorageChangeResponse, error) {
		f, err := s.Provider.StorageChange(ctx, m)
		if err != nil {
			return nil, err
		}
		scoped := proto.Clone(m).(*v1.GetStorageChangeRequest)
		scoped.System = s.Ref.GetId()
		return FinishStorageChange(scoped, f)
	})
}

func (h *Handler) ListMetricContributors(ctx context.Context, req *connect.Request[v1.ListMetricContributorsRequest]) (*connect.Response[v1.ListMetricContributorsResponse], error) {
	return call(ctx, h, "ListMetricContributors", req.Msg, func(p Provider, m *v1.ListMetricContributorsRequest) error {
		return NormalizeMetricContributors(m, rangeReference(p))
	}, on(Provider.ListMetricContributors))
}

func (h *Handler) ListFindingEpisodes(ctx context.Context, req *connect.Request[v1.ListFindingEpisodesRequest]) (*connect.Response[v1.ListFindingEpisodesResponse], error) {
	return call(ctx, h, "ListFindingEpisodes", req.Msg, func(p Provider, m *v1.ListFindingEpisodesRequest) error {
		_, err := NormalizeFindingEpisodes(m, rangeReference(p))
		return err
	}, on(Provider.ListFindingEpisodes))
}

func (h *Handler) GetEventBatch(ctx context.Context, req *connect.Request[v1.GetEventBatchRequest]) (*connect.Response[v1.GetEventBatchResponse], error) {
	return call(ctx, h, "GetEventBatch", req.Msg, func(_ Provider, m *v1.GetEventBatchRequest) error { return EventBatchSelection(m.Ids, m.EventReadId) }, on(Provider.GetEventBatch))
}

func (h *Handler) ListEventSelection(ctx context.Context, req *connect.Request[v1.ListEventSelectionRequest]) (*connect.Response[v1.ListEventSelectionResponse], error) {
	return call(ctx, h, "ListEventSelection", req.Msg, func(_ Provider, m *v1.ListEventSelectionRequest) error { return NormalizeEventSelection(m) }, on(Provider.ListEventSelection))
}
