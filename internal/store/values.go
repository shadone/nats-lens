package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"
)

// i64 converts a counter to BIGINT; the driver rejects uint64 values above
// the int64 range, so such values are capped.
func i64(u uint64) int64 {
	if u > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(u)
}

// ts returns a UTC time, or NULL for the zero time.
func ts(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// tsp is ts for an optional time.
func tsp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

// durNS parses a Go duration string such as an RTT ("1.2ms") into
// nanoseconds, or NULL when empty or unparsable.
func durNS(s string) any {
	if s == "" {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil
	}
	return d.Nanoseconds()
}

// jsonText marshals v to a JSON string for a JSON column, or NULL for nil.
func jsonText(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return string(b)
}

// enum returns the JSON spelling of a nats-server enum ("limits",
// "workqueue", "explicit"), which is what the API and CLI show.
func enum(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.Trim(string(b), `"`)
}

// configHash identifies a configuration. The creation time is left out, so
// that replicas built at different times do not show as a configuration
// difference; configVersion compares the creation time on its own.
func configHash(cfg any) string {
	b, _ := json.Marshal(cfg)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// strs returns a non-nil copy so that an empty list is stored as [] and not NULL.
func strs(xs []string) []string {
	out := make([]string, 0, len(xs))
	return append(out, xs...)
}

// durs converts durations to nanoseconds.
func durs(ds []time.Duration) []int64 {
	out := make([]int64, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Nanoseconds())
	}
	return out
}
