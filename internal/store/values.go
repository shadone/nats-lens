package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
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
// difference; writeConfigVersions compares the creation time on its own.
func configHash(cfg any) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12]), nil
}

// repeatHash is the configuration's part of the key repeated compares: its
// hash, or the error for one that does not encode, so that two such
// reports compare by their errors and not as equal configurations.
func repeatHash(hash string, err error) string {
	if err != nil {
		return "no hash: " + err.Error()
	}
	return hash
}

// unhashed notes, in the epoch's errors, a configuration that does not
// encode as JSON, and logs it once per process; the writers store no
// configuration version for it. A configuration that does not encode
// because it holds a value the server sent and this nats-server version
// does not know gets no note of its own: unknownNotes writes one per
// server, field and value, and the collector logs it.
func (w *writer) unhashed(v *collect.ServerView, key assetKey, cfg any, err error) {
	if collect.HoldsUnknown(cfg) {
		return
	}
	w.notes = append(w.notes, fmt.Sprintf("%s reported %s with a configuration that does not encode as JSON (%v); its configuration is not stored",
		v.Info.Name, key, err))
	if w.firstWarning(warnUnhashed, key) {
		w.log.Warn("configuration does not encode as JSON, not stored; logged once per process",
			"server", v.Info.Name, "asset", key.String(), "error", err)
	}
}

// unknownNotes notes, in the epoch's errors, each value of a stream or
// consumer configuration that the server's JSZ reply sent and this
// nats-server version does not know: one note per field, value and what
// became of the configurations that held it, with how many there were. A
// configuration is stored only when every unknown value it holds encodes
// (a deliver policy as "undefined"); when one does not, every note for it
// says it is not stored.
func (w *writer) unknownNotes(v *collect.ServerView) {
	type group struct {
		kind, field, value, encoded string
	}
	type held struct {
		kind, field string
		u           collect.UnknownValue
	}
	unknown := v.Status[collect.Jsz].Unknown
	// The configurations, by the path of their "config" object, and
	// whether each is stored.
	stored := map[string]bool{}
	var fields []held
	var configs []string
	for _, u := range unknown {
		f := u.Field()
		var h held
		switch {
		case strings.HasPrefix(f, "account_details[].stream_detail[].consumer_detail[].config."):
			h = held{"consumer", strings.TrimPrefix(f, "account_details[].stream_detail[].consumer_detail[]."), u}
		case strings.HasPrefix(f, "account_details[].stream_detail[].config."):
			h = held{"stream", strings.TrimPrefix(f, "account_details[].stream_detail[]."), u}
		default:
			continue
		}
		cfg := configPath(u.Path)
		if _, ok := stored[cfg]; !ok {
			stored[cfg] = true
		}
		stored[cfg] = stored[cfg] && u.Encoded != ""
		fields = append(fields, h)
		configs = append(configs, cfg)
	}
	counts := map[group]int{}
	var order []group
	for i, h := range fields {
		g := group{h.kind, h.field, h.u.Value, ""}
		if stored[configs[i]] {
			g.encoded = h.u.Encoded
		}
		if counts[g] == 0 {
			order = append(order, g)
		}
		counts[g]++
	}
	for _, g := range order {
		n := counts[g]
		what := g.kind + "s"
		outcome := "their configurations are not stored"
		if n == 1 {
			what, outcome = g.kind, "its configuration is not stored"
		}
		if g.encoded != "" {
			outcome = fmt.Sprintf("stored as %s", g.encoded)
		}
		w.notes = append(w.notes, fmt.Sprintf("%s sent %s %s %s, which this nats-server version does not know, for %d %s; %s",
			v.Info.Name, g.kind, g.field, g.value, n, what, outcome))
	}
}

// configPath is the path of the configuration object that holds the
// field at path: everything before its last ".config.".
func configPath(path string) string {
	if i := strings.LastIndex(path, ".config."); i >= 0 {
		return path[:i]
	}
	return path
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
