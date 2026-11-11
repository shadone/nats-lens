// Package storeprov answers the API from the collector's DuckDB database:
// servers, streams, consumers, connections, events and time series from
// the snapshots and events, findings, their transitions and the status
// timeline from the checks, and mutes.
package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// Provider reads the store. Every query is a read; the only writes are
// mutes, through the store's own methods.
type Provider struct {
	listPages    api.EntityPages
	episodePages api.EpisodePages
	St           *store.Store
	// Interval is the collector's time between epochs: the shortest series
	// step and the freshness interval.
	Interval time.Duration
	// SubscriptionsInterval is the collector's subscriptions_interval; 0
	// when not known, and then api.DefaultSubscriptionsInterval bounds
	// the delivered rates.
	SubscriptionsInterval time.Duration
	// Live is whether a collector writes to the store in this process.
	Live bool
	// CapturedOnly excludes runtime state in offline investigations.
	CapturedOnly bool
	// Retention is how long the collector keeps epochs and events.
	Retention time.Duration
	// RetentionKnown is explicit captured metadata; bare offline files lack it.
	RetentionKnown bool
	// Clock returns now; nil means the wall clock.
	Clock func() time.Time
	// RangeClock overrides only derived half-open query bounds, not observed time.
	RangeClock func() time.Time
	// Checks names the checks and gives their remediation; nil leaves
	// those fields empty.
	Checks *checks.Set
	// ToolingNames are nats-lens's own connection names; such connections,
	// and clients from a loopback address, are marked as tooling.
	ToolingNames []string
	// LoopbackNotTooling turns off the rule that loopback clients are
	// tooling (checks.loopback_is_tooling: false).
	LoopbackNotTooling bool
	// ClientPods matches the names of pods that are NATS clients besides
	// those a connection name names; their failures are drawn on the
	// Kubernetes lane. Nil matches none.
	ClientPods *regexp.Regexp
	// Kubernetes is whether nats-lens watches Kubernetes for this system
	// (kubernetes.enabled); without it Replay leaves the node counts
	// unset.
	Kubernetes bool
	// ClientPodWatch is whether nats-lens watches client pods for this
	// system (kubernetes.client_pod_watch); without it Applications leaves
	// Runs on unset and reads no client pod.
	ClientPodWatch bool
	// ArchiveStatus reports the archiver's state for Data history; nil
	// when no archiver runs, as in natslens once and natslens sql.
	ArchiveStatus func() api.ArchiveState

	podCache podNameCache
	readDB   queryReader
}

var _ api.Provider = (*Provider)(nil)

// CollectInterval is the collector's time between epochs.
func (p *Provider) CollectInterval() time.Duration { return p.Interval }

// Now is the wall clock in UTC.
func (p *Provider) Now() time.Time {
	if p.Clock != nil {
		return p.Clock().UTC()
	}
	return time.Now().UTC()
}

// RangeReference is used only to resolve omitted range bounds. Explicit bounds
// and Now's observed dataset clock remain unchanged.
func (p *Provider) RangeReference() time.Time {
	if p.RangeClock != nil {
		return p.RangeClock().UTC()
	}
	return p.Now()
}

type queryReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (p *Provider) db() queryReader {
	if p.readDB != nil {
		return p.readDB
	}
	return p.St.DB()
}

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func dur(d time.Duration) *durationpb.Duration { return durationpb.New(d) }

func span(r *v1.TimeRange) (time.Time, time.Time) {
	return r.GetStart().AsTime().UTC(), r.GetEnd().AsTime().UTC()
}

// epochInfo is the newest epoch of a range.
type epochInfo struct {
	epoch     int64
	startedAt time.Time
	partial   bool
	expected  []string
	missing   []string
}

// latestEpoch returns the newest epoch that started inside the range; ok
// is false when the range holds none.
func (p *Provider) latestEpoch(ctx context.Context, r *v1.TimeRange) (epochInfo, bool, error) {
	start, end := span(r)
	var e epochInfo
	var expected, missing []any
	err := p.db().QueryRowContext(ctx, `
SELECT epoch, started_at, partial, expected_names, missing_names FROM epochs
WHERE started_at >= ? AND started_at < ? ORDER BY epoch DESC LIMIT 1`, start, end).
		Scan(&e.epoch, &e.startedAt, &e.partial, &expected, &missing)
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	if err != nil {
		return e, false, err
	}
	e.startedAt = e.startedAt.UTC()
	e.expected, e.missing = strs(expected), strs(missing)
	return e, true, nil
}

// strs converts a scanned DuckDB VARCHAR[] to strings.
func strs(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func nstr(s sql.NullString) string { return s.String }

func ntime(t sql.NullTime) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time.UTC()
}

// Info reports the collector's settings, the cluster name of the newest
// epoch's servers and the start of the oldest stored epoch.
func (p *Provider) Info(ctx context.Context) (*v1.GetLensInfoResponse, error) {
	res := &v1.GetLensInfoResponse{CollectInterval: dur(p.Interval), Retention: dur(p.Retention), Live: p.Live, NowFixed: p.CapturedOnly}
	if p.CapturedOnly && !p.RetentionKnown {
		res.Retention = nil
	}
	var cluster sql.NullString
	err := p.db().QueryRowContext(ctx, `
SELECT any_value(s.cluster) FROM servers s
WHERE s.last_epoch = (SELECT max(epoch) FROM epochs) AND s.cluster IS NOT NULL AND s.cluster <> ''`).Scan(&cluster)
	if err != nil {
		return nil, err
	}
	res.Cluster = cluster.String
	var start sql.NullTime
	if err := p.db().QueryRowContext(ctx, `SELECT min(started_at) FROM epochs`).Scan(&start); err != nil {
		return nil, err
	}
	res.HistoryStart = ts(ntime(start))
	return res, nil
}

// AccountNames returns the readable name of every account that has one
// other than its identifier.
func (p *Provider) AccountNames(ctx context.Context) (map[string]string, error) {
	rows, err := p.db().QueryContext(ctx, `SELECT account, account_name FROM accounts
WHERE account_name IS NOT NULL AND account_name <> account`)
	if err != nil {
		return nil, fmt.Errorf("account names: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var acc, name string
		if err := rows.Scan(&acc, &name); err != nil {
			return nil, err
		}
		out[acc] = name
	}
	return out, rows.Err()
}
