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

// Provider answers the API's calls for one system. It never reads a
// request's system field: the handler has already resolved it to this
// provider. The handlers pass requests whose ranges, page sizes and other
// defaults are already filled in (see normalize.go), so a provider reads
// them as given: every TimeRange has both start and end set.
type Provider interface {
	// Now is the provider's present: the wall clock for the store, the
	// scenario's fixed time for the fake. Ranges without an end end here.
	Now() time.Time

	// Info returns the cluster name, collect interval, retention, and
	// whether the data is live or the demo; the handler adds the system,
	// the identity mode and the version.
	Info(ctx context.Context) (*v1.GetLensInfoResponse, error)

	// CollectInterval is the time between epochs; the default range of
	// GetShellStatus is two of them.
	CollectInterval() time.Duration

	GetOverview(ctx context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error)
	// ShellStatus returns the freshness, servers and finding tally of the
	// range, as GetOverview reads them; the handler adds the systems.
	ShellStatus(ctx context.Context, req *v1.GetShellStatusRequest) (*v1.GetShellStatusResponse, error)
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
	// GetPlacement returns every stream and durable consumer of the range's
	// newest epoch with its cells, unfiltered and in any order; the
	// handler applies the filter and counts with FinishPlacement.
	GetPlacement(ctx context.Context, req *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error)
	// Replay reads the facts of the story between the request's A and B,
	// which the handler has checked and clamped to Now; the handler words
	// them with FinishReplay. A older than the retention is ErrInvalid.
	Replay(ctx context.Context, req *v1.GetReplayRequest) (*ReplayFacts, error)
	// Topology reads the rows of the range's newest epoch and the drop
	// window before it; the handler pairs, counts and orders them with
	// FinishTopology. A range without an epoch gives facts with Epoch 0.
	Topology(ctx context.Context, req *v1.GetTopologyRequest) (*TopologyFacts, error)
	ListServers(ctx context.Context, req *v1.ListServersRequest) (*v1.ListServersResponse, error)
	GetServer(ctx context.Context, req *v1.GetServerRequest) (*v1.GetServerResponse, error)
	ListConnections(ctx context.Context, req *v1.ListConnectionsRequest) (*v1.ListConnectionsResponse, error)
	GetTimeSeries(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error)

	// SystemSummary summarizes the provider's system for ListSystems; the
	// handler sets its system reference.
	SystemSummary(ctx context.Context, req *v1.ListSystemsRequest) (*v1.SystemSummary, error)
	// ListAccounts lists the accounts of the newest epoch in the range.
	ListAccounts(ctx context.Context, req *v1.ListAccountsRequest) (*v1.ListAccountsResponse, error)
	ListKeyValueStores(ctx context.Context, req *v1.ListKeyValueStoresRequest) (*v1.ListKeyValueStoresResponse, error)
	ListObjectStores(ctx context.Context, req *v1.ListObjectStoresRequest) (*v1.ListObjectStoresResponse, error)
}
