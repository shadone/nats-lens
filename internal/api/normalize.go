package api

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Defaults and limits applied before a request reaches the provider.
const (
	DefaultRange         = 24 * time.Hour
	MaxRange             = 400 * 24 * time.Hour
	DefaultBucket        = 15 * time.Minute
	MaxBuckets           = 2000
	DefaultSeriesPoints  = 200
	DefaultOverviewPoint = 96
	MaxSeriesPoints      = 2000
	DefaultPageSize      = 100
	MaxFindingsPage      = 500
	MaxMutesPage         = 500
	MaxEventsPage        = 1000
	MaxConnectionsPage   = 1000
	MaxEnumFilters       = 10
	MaxKindFilters       = 50
	MaxReasonLength      = 1000
	MaxNameLength        = 256
	MinMuteLength        = time.Minute
	MaxMuteLength        = 90 * 24 * time.Hour
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ResolveRange returns r with start and end set: an unset end is now, an
// unset start is end minus def. A range spans at most MaxRange.
func ResolveRange(r *v1.TimeRange, now time.Time, def time.Duration) (*v1.TimeRange, error) {
	end := now
	if r.GetEnd() != nil {
		if err := r.GetEnd().CheckValid(); err != nil {
			return nil, invalid("range.end: %v", err)
		}
		end = r.GetEnd().AsTime()
	}
	start := end.Add(-def)
	if r.GetStart() != nil {
		if err := r.GetStart().CheckValid(); err != nil {
			return nil, invalid("range.start: %v", err)
		}
		start = r.GetStart().AsTime()
	}
	if !start.Before(end) {
		return nil, invalid("range.start must be before range.end")
	}
	// Compare in two steps: Sub saturates for spans beyond 292 years.
	if end.Sub(start) > MaxRange || start.Before(end.Add(-MaxRange)) {
		return nil, invalid("range spans more than %d days", MaxRange/(24*time.Hour))
	}
	return &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(end)}, nil
}

// pageSize applies the default and the limit to a requested page size.
func pageSize(n, max int32) (int32, error) {
	switch {
	case n < 0:
		return 0, invalid("page_size must not be negative")
	case n == 0:
		return DefaultPageSize, nil
	case n > max:
		return max, nil
	}
	return n, nil
}

func points(n, def int32) (int32, error) {
	switch {
	case n < 0:
		return 0, invalid("max_points must not be negative")
	case n == 0:
		return def, nil
	case n > MaxSeriesPoints:
		return MaxSeriesPoints, nil
	}
	return n, nil
}

// maxLen rejects a filter list longer than max.
func maxLen(name string, n, max int) error {
	if n > max {
		return invalid("%s has %d entries, at most %d", name, n, max)
	}
	return nil
}

// ceilDiv is a divided by b, rounded up, for non-negative a and positive
// b. It adds the remainder's bucket instead of adding b-1 to a, which
// overflows for a b near the largest Duration.
func ceilDiv(a, b time.Duration) int64 {
	n := int64(a / b)
	if a%b != 0 {
		n++
	}
	return n
}

// BucketCount is the number of buckets of width bucket in a span, the last
// one counted even when partial.
func BucketCount(span, bucket time.Duration) int { return int(ceilDiv(span, bucket)) }

func normalizeTimeline(req *v1.GetStatusTimelineRequest, now time.Time) error {
	r, err := ResolveRange(req.GetRange(), now, DefaultRange)
	if err != nil {
		return err
	}
	req.Range = r
	bucket := DefaultBucket
	if req.GetBucket() != nil {
		if err := req.GetBucket().CheckValid(); err != nil {
			return invalid("bucket: %v", err)
		}
		bucket = req.GetBucket().AsDuration()
	}
	if bucket < time.Second {
		return invalid("bucket must be at least one second")
	}
	span := r.GetEnd().AsTime().Sub(r.GetStart().AsTime())
	if n := ceilDiv(span, bucket); n > MaxBuckets {
		return invalid("range holds %d buckets of %s, at most %d", n, bucket, MaxBuckets)
	}
	req.Bucket = durationpb.New(bucket)
	return checkEntity("entity", req.GetEntity(), true)
}

func normalizeTimeSeries(req *v1.GetTimeSeriesRequest, now time.Time) error {
	m, ok := Metrics[req.GetMetric()]
	if !ok {
		return invalid("unknown metric %q", req.GetMetric())
	}
	if m.Entity == v1.EntityType_ENTITY_TYPE_SYSTEM {
		if t := req.GetEntity().GetType(); t != v1.EntityType_ENTITY_TYPE_UNSPECIFIED && t != v1.EntityType_ENTITY_TYPE_SYSTEM {
			return invalid("metric %s takes no entity", req.GetMetric())
		}
		req.Entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	} else if req.GetEntity().GetType() != m.Entity {
		return invalid("metric %s needs an entity of type %s", req.GetMetric(), m.Entity)
	} else if err := checkEntity("entity", req.GetEntity(), false); err != nil {
		return err
	}
	r, err := ResolveRange(req.GetRange(), now, DefaultRange)
	if err != nil {
		return err
	}
	req.Range = r
	req.MaxPoints, err = points(req.GetMaxPoints(), DefaultSeriesPoints)
	return err
}

var checkCodePattern = regexp.MustCompile(`^[a-z]+-\d{3}$`)

// text trims s and checks that it is set and not longer than max runes.
func text(name, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", invalid("%s is required", name)
	case utf8.RuneCountInString(s) > max:
		return "", invalid("%s is longer than %d characters", name, max)
	}
	return s, nil
}

// normalizeMute checks a mute request. user is the verified IAP user when
// the server is configured for IAP, and empty otherwise; it replaces the
// name in the request.
func normalizeMute(req *v1.MuteFindingRequest, now time.Time, user string) error {
	if !checkCodePattern.MatchString(req.GetCheckCode()) {
		return invalid("check_code %q is not of the form <entity>-<three digits>", req.GetCheckCode())
	}
	var err error
	if req.Reason, err = text("reason", req.GetReason(), MaxReasonLength); err != nil {
		return err
	}
	if req.GetExpiresAt() == nil {
		return invalid("expires_at is required: a mute without an expiry is not allowed")
	}
	if err := req.GetExpiresAt().CheckValid(); err != nil {
		return invalid("expires_at: %v", err)
	}
	exp := req.GetExpiresAt().AsTime().Truncate(time.Second)
	switch {
	case exp.Before(now.Add(MinMuteLength)):
		return invalid("expires_at must be at least %s ahead", MinMuteLength)
	case exp.After(now.Add(MaxMuteLength)):
		return invalid("expires_at must be at most %d days ahead", MaxMuteLength/(24*time.Hour))
	}
	req.ExpiresAt = timestamppb.New(exp)
	if req.GetEntity() != nil {
		if err := checkEntity("entity", req.GetEntity(), false); err != nil {
			return err
		}
	}
	if user != "" {
		req.MutedBy = user
	}
	req.MutedBy, err = text("muted_by", req.GetMutedBy(), MaxNameLength)
	return err
}

func normalizeUnmute(req *v1.UnmuteFindingRequest, user string) error {
	if req.GetMuteId() == "" {
		return invalid("mute_id is required")
	}
	if user != "" {
		req.UnmutedBy = user
	}
	var err error
	req.UnmutedBy, err = text("unmuted_by", req.GetUnmutedBy(), MaxNameLength)
	return err
}

// normalizeReplay checks a Replay request: A and B are set and valid, B
// after now is now, A is before B, and B is at most MaxReplaySpan after
// A.
func normalizeReplay(m *v1.GetReplayRequest, now time.Time) error {
	if m.GetA() == nil || m.GetB() == nil {
		return invalid("a and b are required")
	}
	if err := m.GetA().CheckValid(); err != nil {
		return invalid("a: %v", err)
	}
	if err := m.GetB().CheckValid(); err != nil {
		return invalid("b: %v", err)
	}
	if m.GetB().AsTime().After(now) {
		m.B = timestamppb.New(now)
	}
	a, b := m.GetA().AsTime(), m.GetB().AsTime()
	if !a.Before(b) {
		return invalid("a must be before b, and before now")
	}
	if b.Sub(a) > MaxReplaySpan {
		return invalid("a and b are at most %g hours apart", MaxReplaySpan.Hours())
	}
	return nil
}
