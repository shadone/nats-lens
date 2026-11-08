// Package dataset acquires and validates trusted offline investigation datasets.
// Integrity hashes detect damage; they do not authenticate a dataset or sandbox SQL.
package dataset

import (
	"database/sql"
	"errors"
	"time"
)

const FormatVersion = 1
const ManifestFile = "manifest.json"
const DataFile = "data.duckdb"
const FullSystemScope = "full_retained_system"

var (
	ErrInvalid     = errors.New("invalid dataset")
	ErrVersion     = errors.New("incompatible dataset version")
	ErrIntegrity   = errors.New("dataset integrity failure")
	ErrBusy        = errors.New("dataset acquisition already running")
	ErrBudget      = errors.New("dataset acquisition budget exceeded")
	ErrSpace       = errors.New("insufficient destination space")
	ErrExists      = errors.New("dataset destination exists")
	ErrNoReference = errors.New("no_reference_clock")
)

// All int64 values use decimal JSON strings so consumers preserve exact values.
type System struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	CollectionIntervalNS int64  `json:"collection_interval_ns,string"`
	RetentionNS          *int64 `json:"retention_ns,string"`
}

type Boundaries struct {
	FirstEpoch              *int64     `json:"first_epoch,string"`
	LatestEpoch             *int64     `json:"latest_epoch,string"`
	FirstCollectionAt       *time.Time `json:"first_collection_at"`
	LastCollectionStartedAt *time.Time `json:"last_collection_started_at"`
	DataReference           *time.Time `json:"data_reference"`
	FirstEventAt            *time.Time `json:"first_event_at"`
	LastEventAt             *time.Time `json:"last_event_at"`
	LastEventReceivedAt     *time.Time `json:"last_event_received_at"`
	LatestEventID           *int64     `json:"latest_event_id,string"`
	CheckRuns               int64      `json:"check_runs,string"`
	LatestEvaluatedEpoch    *int64     `json:"latest_evaluated_epoch,string"`
	// PendingEvaluationEpochs counts collections without any published check run.
	// A nonzero count of check runs does not establish whole-pipeline completion.
	PendingEvaluationEpochs int64 `json:"pending_evaluation_epochs,string"`
}

type Data struct {
	Path   string `json:"path"`
	Size   int64  `json:"size,string"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	FormatVersion      int        `json:"format_version"`
	Scope              string     `json:"scope"`
	System             System     `json:"system"`
	ApplicationVersion string     `json:"application_version"`
	SchemaVersion      int        `json:"schema_version"`
	EngineVersion      string     `json:"engine_version"`
	CaptureStartedAt   time.Time  `json:"capture_started_at"`
	ExportedAt         time.Time  `json:"exported_at"`
	Boundaries         Boundaries `json:"boundaries"`
	// Complete describes successful artifact publication, not completeness of monitoring.
	Complete            bool     `json:"complete"`
	PendingStages       []string `json:"pending_stages"`
	ExportedFacts       []string `json:"exported_facts"`
	LiveOnly            []string `json:"live_only"`
	ChecksConfiguration string   `json:"checks_configuration"`
	Data                Data     `json:"data"`
}

type Dataset struct {
	Manifest Manifest
	DB       *sql.DB
	DBPath   string
}

func (d *Dataset) Close() error { return d.DB.Close() }
