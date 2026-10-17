// Package api serves the natslens.v1.LensService Connect API. The handlers
// validate and complete each request, then call a Provider: the store
// provider reads the collector's database, the fake provider serves a fixed
// scenario for the demo and for tests.
package api

import (
	"context"
	"errors"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Errors a provider returns; the handlers map them to Connect codes.
var (
	ErrNotFound           = errors.New("not found")
	ErrInvalid            = errors.New("invalid argument")
	ErrFailedPrecondition = errors.New("failed precondition")
)

// Provider answers the API's calls. The handlers pass requests whose
// ranges, page sizes and other defaults are already filled in (see
// normalize.go), so a provider reads them as given: every TimeRange has
// both start and end set.
type Provider interface {
	// Now is the provider's present: the wall clock for the store, the
	// scenario's fixed time for the fake. Ranges without an end end here.
	Now() time.Time

	// Info returns the cluster name, collect interval, retention, and
	// whether the data is live or the demo; the handler adds the identity
	// mode and the version.
	Info(ctx context.Context) (*v1.GetLensInfoResponse, error)

	GetOverview(ctx context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error)
	ListFindings(ctx context.Context, req *v1.ListFindingsRequest) (*v1.ListFindingsResponse, error)
	GetFinding(ctx context.Context, req *v1.GetFindingRequest) (*v1.GetFindingResponse, error)
	// MuteFinding stores a mute; the handler has checked the reason, the
	// expiry and set muted_by.
	MuteFinding(ctx context.Context, req *v1.MuteFindingRequest) (*v1.Mute, error)
	// UnmuteFinding lifts a mute; ErrNotFound for an unknown ID,
	// ErrFailedPrecondition for one that no longer applies.
	UnmuteFinding(ctx context.Context, req *v1.UnmuteFindingRequest) (*v1.Mute, error)
	ListMutes(ctx context.Context, req *v1.ListMutesRequest) (*v1.ListMutesResponse, error)
	GetStatusTimeline(ctx context.Context, req *v1.GetStatusTimelineRequest) (*v1.GetStatusTimelineResponse, error)
	ListEvents(ctx context.Context, req *v1.ListEventsRequest) (*v1.ListEventsResponse, error)
	GetEvent(ctx context.Context, req *v1.GetEventRequest) (*v1.GetEventResponse, error)
	ListStreams(ctx context.Context, req *v1.ListStreamsRequest) (*v1.ListStreamsResponse, error)
	GetStream(ctx context.Context, req *v1.GetStreamRequest) (*v1.GetStreamResponse, error)
	ListConsumers(ctx context.Context, req *v1.ListConsumersRequest) (*v1.ListConsumersResponse, error)
	GetConsumer(ctx context.Context, req *v1.GetConsumerRequest) (*v1.GetConsumerResponse, error)
	ListServers(ctx context.Context, req *v1.ListServersRequest) (*v1.ListServersResponse, error)
	GetServer(ctx context.Context, req *v1.GetServerRequest) (*v1.GetServerResponse, error)
	ListConnections(ctx context.Context, req *v1.ListConnectionsRequest) (*v1.ListConnectionsResponse, error)
	GetTimeSeries(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error)
}
