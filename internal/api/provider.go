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
	ErrResourceExhausted  = errors.New("resource exhausted")
	ErrInvalid            = errors.New("invalid argument")
	ErrFailedPrecondition = errors.New("failed precondition")
)

// Provider answers the API's calls for one system. It never reads a
// request's system field: the handler has already resolved it to this
// provider. The handlers pass requests whose ranges, page sizes and other
// defaults are already filled in (see normalize.go), so a provider reads
// them as given: every TimeRange has both start and end set.
type Provider interface {
	ListMetricContributors(context.Context, *v1.ListMetricContributorsRequest) (*v1.ListMetricContributorsResponse, error)
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
	ListFindingEpisodes(ctx context.Context, req *v1.ListFindingEpisodesRequest) (*v1.ListFindingEpisodesResponse, error)
	GetFinding(ctx context.Context, req *v1.GetFindingRequest) (*v1.GetFindingResponse, error)
	// OnCall reads the card's present facts. The handler reads and finishes Replay.
	OnCall(ctx context.Context, req *v1.GetOnCallRequest) (*OnCallFacts, error)
	// MuteFinding stores a mute; the handler has checked the reason, the
	// expiry and set muted_by.
	MuteFinding(ctx context.Context, req *v1.MuteFindingRequest) (*v1.Mute, error)
	// UnmuteFinding lifts a mute; ErrNotFound for an unknown ID,
	// ErrFailedPrecondition for one that no longer applies.
	UnmuteFinding(ctx context.Context, req *v1.UnmuteFindingRequest) (*v1.Mute, error)
	ListMutes(ctx context.Context, req *v1.ListMutesRequest) (*v1.ListMutesResponse, error)
	// AcknowledgeFinding stores an acknowledgement of the finding's
	// current episode; the handler has checked the note and the quiet and
	// set acked_by. ErrNotFound for an unknown finding,
	// ErrFailedPrecondition for one that does not fire.
	AcknowledgeFinding(ctx context.Context, req *v1.AcknowledgeFindingRequest) (*v1.Acknowledgement, error)
	// EndAcknowledgement hands an acknowledgement back; ErrNotFound for
	// an unknown ID, ErrFailedPrecondition for one no longer in force.
	EndAcknowledgement(ctx context.Context, req *v1.EndAcknowledgementRequest) (*v1.Acknowledgement, error)
	GetStatusTimeline(ctx context.Context, req *v1.GetStatusTimelineRequest) (*v1.GetStatusTimelineResponse, error)
	ListEvents(ctx context.Context, req *v1.ListEventsRequest) (*v1.ListEventsResponse, error)
	GetEvent(ctx context.Context, req *v1.GetEventRequest) (*v1.GetEventResponse, error)
	GetEventBatch(ctx context.Context, req *v1.GetEventBatchRequest) (*v1.GetEventBatchResponse, error)
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
	// AccountsAndAuth reads the accounts of the range's newest epoch with
	// their newest lookups, the failed logins of the 24 hours before it
	// grouped, and the epoch's certificates and user JWTs; the handler
	// orders, limits and counts them with FinishAccounts. A range without
	// an epoch gives facts with Epoch 0.
	AccountsAndAuth(ctx context.Context, req *v1.GetAccountsAndAuthRequest) (*AccountsFacts, error)
	// Capacity reads the server stores and streams of the range's newest
	// epoch with their growth over the range, and the firing findings of
	// the epoch; the handler words, orders and cuts them with
	// FinishCapacity. A range without an epoch gives facts with Epoch 0.
	Capacity(ctx context.Context, req *v1.GetCapacityRequest) (*CapacityFacts, error)
	// StorageChange reads one transaction-pinned comparison; the handler ranks and pages it.
	StorageChange(ctx context.Context, req *v1.GetStorageChangeRequest) (*StorageChangeFacts, error)
	// Subjects reads the streams and consumers of the range's newest
	// epoch, the JetStream API rates of the accounts that use JetStream,
	// and the newest subscription collection at or before the epoch within
	// SubscriptionsMaxAge, grouped by GroupSubscriptions' rules; it may
	// leave out what lies outside the request's account. The handler
	// builds the rows, orders, counts and cuts them with FinishSubjects. A
	// range without an epoch gives facts with Epoch 0.
	Subjects(ctx context.Context, req *v1.GetSubjectsRequest) (*SubjectsFacts, error)
	// MatchSubject reads what GetSubjects reads for the tester: the
	// streams with their consumers, and the subscription collection's
	// CoreGroups, at least those whose subject matches the request's
	// literal subject, which the handler has checked. The handler keeps
	// what matches with FinishMatch.
	MatchSubject(ctx context.Context, req *v1.MatchSubjectRequest) (*MatchFacts, error)
	// MicroServices reads the micro services of the range's newest epoch:
	// the accounts asked in it with their newest $SRV.INFO and the epoch's
	// $SRV.STATS requests, the services that answered with their endpoints'
	// rates (MicroWindowIncrease), levels (MicroLevel) and slow runs
	// (MicroSlowSince), the services seen in the newest subscription
	// collection at or before the epoch within SubscriptionsMaxAge
	// (SeenMicro), and the instances gone within MicroMissingWindow; it may
	// leave out what lies outside the request's account. The handler merges,
	// orders and cuts them with FinishMicro. A range without an epoch gives
	// facts with Epoch 0.
	MicroServices(ctx context.Context, req *v1.GetMicroServicesRequest) (*MicroFacts, error)
	// Applications reads the client connections of the range's newest
	// epoch with their tooling marked, whether a server's list was cut, the
	// client events of the ApplicationDay before it counted by name
	// (AppEventFact), the hourly traffic of its TrafficHours and the findings
	// firing at it; it may leave out what lies outside the request's
	// account. The handler groups, counts and orders them with
	// FinishApplications. A range without an epoch gives facts with Epoch 0.
	Applications(ctx context.Context, req *v1.GetApplicationsRequest) (*ApplicationFacts, error)
	// DataHistory reads the history and event starts, the range's
	// collections and gaps (CollectionGaps), the collector events of
	// HistoryEventKinds around the range, the archive's state, the checks'
	// lookbacks and the findings already true at the history start; the
	// handler applies the gap rules, the coverage and the uses table with
	// FinishDataHistory.
	DataHistory(ctx context.Context, req *v1.GetDataHistoryRequest) (*HistoryFacts, error)
	GetSlowConsumerInvestigation(ctx context.Context, req *v1.GetSlowConsumerInvestigationRequest) (*v1.GetSlowConsumerInvestigationResponse, error)
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
