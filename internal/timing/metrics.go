package timing

// The families /metrics writes. Their label values are closed sets:
// procedure is a LensService method name, code a Connect code, system a
// configured system ID (for an API call, AllSystems or UnknownSystem when
// the call names no one system), source and job the constants below, op a
// store write operation named in the store package. system is always the
// first label, as in every other family of /metrics.
var (
	APIDuration = NewHistogramVec("nats_lens_api_request_duration_seconds",
		"Duration of an API call, from the handler's start to its response, by system, procedure and Connect code (ok for success).",
		[]string{"system", "procedure", "code"}, LatencyBuckets)
	APIQueries = NewHistogramVec("nats_lens_api_request_queries",
		"Database queries and statements one API call ran, every system's counted.",
		[]string{"system", "procedure"}, CountBuckets)
	APIQuerySeconds = NewHistogramVec("nats_lens_api_request_query_seconds",
		"Seconds an API call's queries ran, summed; above the call's duration when its queries ran in parallel.",
		[]string{"system", "procedure"}, LatencyBuckets)
	APIInFlight = NewGaugeVec("nats_lens_api_requests_in_flight",
		"API calls running now, by system and procedure.",
		[]string{"system", "procedure"})
	QueryDuration = NewHistogramVec("nats_lens_db_query_duration_seconds",
		"Duration of one database query or statement, from its start until its rows are closed, by the work that ran it.",
		[]string{"system", "source"}, LatencyBuckets)
	LockWait = NewHistogramVec("nats_lens_store_lock_wait_seconds",
		"Seconds a store write waited for the store's write lock, by operation.",
		[]string{"system", "op"}, LatencyBuckets)
	LockHeld = NewHistogramVec("nats_lens_store_lock_held_seconds",
		"Seconds a store write held the store's write lock, by operation; op=\"epoch\" is the epoch write.",
		[]string{"system", "op"}, LatencyBuckets)
	LockWaiters = NewGaugeVec("nats_lens_store_lock_waiters",
		"Store writes waiting for the store's write lock now.",
		[]string{"system"})
	JobDuration = NewHistogramVec("nats_lens_job_duration_seconds",
		"Duration of one run of a background job, by job: collect, check_evaluation, checks, archive_export, retention_sweep.",
		[]string{"system", "job"}, LatencyBuckets)
)

// System label values of an API call that does not name one configured
// system.
const (
	// AllSystems labels a call whose request has no system field, such
	// as ListSystems.
	AllSystems = "all"
	// UnknownSystem labels a call naming a system that is not configured.
	UnknownSystem = "unknown"
)

// Sources of queries, set on a context with WithSource.
const (
	SourceAPI       = "api"
	SourceMetrics   = "metrics"
	SourceEpoch     = "epoch_write"
	SourceChecks    = "checks"
	SourceArchive   = "archive"
	SourceRetention = "retention"
	SourceEvents    = "events"
	SourceNotify    = "notify"
	SourceOther     = "other"
)

// Jobs of JobDuration.
const (
	JobCollect         = "collect"
	JobCheckEvaluation = "check_evaluation"
	JobChecks          = "checks"
	JobArchiveExport   = "archive_export"
	JobRetentionSweep  = "retention_sweep"
)
