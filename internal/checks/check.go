// Package checks runs the SQL checks in checks/*.sql against the store
// after every epoch and hands the failing entities to the store, which
// keeps the findings and their transitions.
//
// A check file starts with a YAML header in a block comment and holds one
// SELECT. The query reads its inputs with getvariable(): the epoch, its
// start time, the window of a trend check, and its thresholds. It returns
// one row per failing entity with the columns entity_key, severity and
// detail, and optionally value, cause_event_id, related_config (a JSON
// object of setting name to value) and related_source.
package checks

import (
	"fmt"
	"io/fs"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/shadone/nats-lens/internal/config"
)

// Check is one check: its header and its query.
type Check struct {
	Code     string `yaml:"code"`
	Name     string `yaml:"name"`
	Entity   string `yaml:"entity"`
	Category string `yaml:"category"`
	// Severity is the severity of a row whose severity column is null.
	Severity string `yaml:"severity"`
	// SeverityRule says in words when a row gets another severity; empty
	// when every row has Severity.
	SeverityRule string `yaml:"severity_rule"`
	// Kind is operational (one epoch) or trend (a window ending at the
	// epoch).
	Kind string `yaml:"kind"`
	// Lookback is the window of a trend check, and the window of an
	// operational check that counts events.
	Lookback      string      `yaml:"lookback"`
	Thresholds    []Threshold `yaml:"thresholds"`
	Description   string      `yaml:"description"`
	Remediation   string      `yaml:"remediation"`
	RelatedConfig []string    `yaml:"related_config"`
	// ValueName says what the value column measures, with its unit, for
	// example "replicas online"; empty when the check returns no value or
	// its meaning is not obvious.
	ValueName string `yaml:"value_name"`
	// ValueBasis says how the value is computed, for example "online peers
	// of the raft group, leader's view"; it needs ValueName.
	ValueBasis string `yaml:"value_basis"`
	// SuppressedBy lists checks of the same entity type: an entity one of
	// them returns in the same epoch is dropped from this check's rows, as
	// the other finding already says it.
	SuppressedBy []string `yaml:"suppressed_by"`

	// SQL is the query after the header.
	SQL string `yaml:"-"`
	// lookback is Lookback parsed; zero when the check has none.
	lookback time.Duration
	// builtin marks a check computed by the engine, with no query.
	builtin bool
}

// Threshold is one tunable number of a check.
type Threshold struct {
	Name        string `yaml:"name"`
	Default     string `yaml:"default"`
	Unit        string `yaml:"unit"`
	Description string `yaml:"description"`
}

// Units a threshold can have.
const (
	UnitCount    = "count"
	UnitRatio    = "ratio"
	UnitBytes    = "bytes"
	UnitDuration = "duration"
)

// Entity types a check can report on, as the mutes table and the API spell
// them.
var entityTypes = map[string]int{
	"server": 1, "account": 1, "stream": 2, "consumer": 3, "client": 0, "system": -1,
}

var (
	codePattern  = regexp.MustCompile(`^([a-z]+)-(\d{3})$`)
	namePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	varPattern   = regexp.MustCompile(`getvariable\(\s*'([^']*)'\s*\)`)
	categories   = map[string]bool{"health": true, "agreement": true, "inventory": true, "consumers": true, "storage": true, "clients": true, "change": true, "engine": true, CategoryExpiry: true}
	severities   = map[string]bool{"info": true, "warning": true, "critical": true}
	units        = map[string]bool{UnitCount: true, UnitRatio: true, UnitBytes: true, UnitDuration: true}
	reservedVars = map[string]bool{"epoch": true, "epoch_time": true, "prev_epoch": true, "window_start": true, "window_epoch": true, "lookback": true, "lookback_text": true, "restart_window": true, "exclude_tooling": true, "tooling_names": true, "loopback_setting": true, "loopback_now": true, "archive_since": true}
)

// ReservedVariables are the variables a check can read, besides its
// thresholds: epoch (BIGINT), epoch_time (its start, TIMESTAMP) and
// prev_epoch (the newest stored epoch before it, 0 when none),
// restart_window (INTERVAL, checks.restart_window), archive_since (when
// this process began exporting to the archive, TIMESTAMP; NULL when it
// does not export, as natslens once does not); and for a
// check with a lookback, lookback (INTERVAL), lookback_text (as text, for
// example 15m), window_start (epoch_time minus lookback) and window_epoch
// (the first epoch that started at or after window_start).
func ReservedVariables() []string {
	out := make([]string, 0, len(reservedVars))
	for v := range reservedVars {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// parse reads one check file: a /* ... */ YAML header, then the query.
func parse(file string, b []byte) (*Check, error) {
	text := strings.TrimSpace(string(b))
	if !strings.HasPrefix(text, "/*") {
		return nil, fmt.Errorf("%s: must start with a /* YAML header */", file)
	}
	end := strings.Index(text, "*/")
	if end < 0 {
		return nil, fmt.Errorf("%s: header comment is not closed", file)
	}
	c := &Check{}
	dec := yaml.NewDecoder(strings.NewReader(text[2:end]))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("%s: header: %w", file, err)
	}
	c.SQL = strings.TrimSpace(text[end+2:])
	if want := c.Code + ".sql"; file != want {
		return nil, fmt.Errorf("%s: file name must be %s", file, want)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return c, nil
}

// validate checks the header and that the query reads only known
// variables and every threshold it declares.
func (c *Check) validate() error {
	m := codePattern.FindStringSubmatch(c.Code)
	if m == nil {
		return fmt.Errorf("code %q is not <entity>-NNN", c.Code)
	}
	if _, ok := entityTypes[c.Entity]; !ok {
		return fmt.Errorf("entity %q is not one of server, account, stream, consumer, client, system", c.Entity)
	}
	if m[1] != c.Entity {
		return fmt.Errorf("code %s does not start with its entity %s", c.Code, c.Entity)
	}
	if c.Name == "" || c.Description == "" || c.Remediation == "" {
		return fmt.Errorf("name, description and remediation are required")
	}
	if c.ValueBasis != "" && c.ValueName == "" {
		return fmt.Errorf("value_basis needs value_name")
	}
	if !categories[c.Category] {
		return fmt.Errorf("unknown category %q", c.Category)
	}
	if !severities[c.Severity] {
		return fmt.Errorf("severity %q is not info, warning or critical", c.Severity)
	}
	switch c.Kind {
	case "operational", "trend":
	default:
		return fmt.Errorf("kind %q is not operational or trend", c.Kind)
	}
	if c.Lookback != "" {
		d, err := config.ParseDuration(c.Lookback)
		if err != nil || d.D() <= 0 {
			return fmt.Errorf("lookback %q is not a positive duration", c.Lookback)
		}
		c.lookback = d.D()
	}
	if c.Kind == "trend" && c.lookback == 0 {
		return fmt.Errorf("a trend check needs a lookback")
	}
	declared := map[string]bool{}
	for _, t := range c.Thresholds {
		if !namePattern.MatchString(t.Name) || reservedVars[t.Name] {
			return fmt.Errorf("threshold name %q is invalid or reserved", t.Name)
		}
		if declared[t.Name] {
			return fmt.Errorf("threshold %s declared twice", t.Name)
		}
		declared[t.Name] = true
		if !units[t.Unit] {
			return fmt.Errorf("threshold %s: unknown unit %q", t.Name, t.Unit)
		}
		if _, err := parseValue(t.Unit, t.Default); err != nil {
			return fmt.Errorf("threshold %s default: %w", t.Name, err)
		}
	}
	if c.builtin {
		return nil
	}
	if c.SQL == "" {
		return fmt.Errorf("no query after the header")
	}
	used := map[string]bool{}
	for _, m := range varPattern.FindAllStringSubmatch(c.SQL, -1) {
		v := m[1]
		if !reservedVars[v] && !declared[v] {
			return fmt.Errorf("the query reads variable %q, which is neither reserved nor a declared threshold", v)
		}
		if (v == "lookback" || v == "lookback_text" || v == "window_start" || v == "window_epoch") && c.lookback == 0 {
			return fmt.Errorf("the query reads %s but the check has no lookback", v)
		}
		used[v] = true
	}
	for name := range declared {
		if !used[name] {
			return fmt.Errorf("threshold %s is declared but the query never reads it", name)
		}
	}
	return nil
}

// Value is a threshold value ready for the query: a number, or a duration
// for UnitDuration.
type Value struct {
	Number   float64
	Duration time.Duration
	Unit     string
}

// literal is the value as a DuckDB literal.
func (v Value) literal() string {
	if v.Unit == UnitDuration {
		return fmt.Sprintf("INTERVAL '%d microseconds'", v.Duration.Microseconds())
	}
	return strconv.FormatFloat(v.Number, 'g', -1, 64) + "::DOUBLE"
}

func (v Value) String() string {
	switch v.Unit {
	case UnitDuration:
		return DurationText(v.Duration)
	case UnitBytes:
		return formatBytes(v.Number)
	}
	return strconv.FormatFloat(v.Number, 'g', -1, 64)
}

var byteSuffixes = []struct {
	suffix string
	mult   float64
}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}}

func formatBytes(n float64) string {
	for _, s := range byteSuffixes {
		if n >= s.mult && n/s.mult == float64(int64(n/s.mult)) {
			return strconv.FormatInt(int64(n/s.mult), 10) + s.suffix
		}
	}
	return strconv.FormatFloat(n, 'g', -1, 64)
}

// parseValue reads a threshold value in its unit: a number for count and
// ratio (a ratio is between 0 and 1), a number with an optional KiB, MiB,
// GiB or TiB suffix for bytes, and a duration ("90s", "15m", "2d").
func parseValue(unit, s string) (Value, error) {
	s = strings.TrimSpace(s)
	v := Value{Unit: unit}
	switch unit {
	case UnitDuration:
		d, err := config.ParseDuration(s)
		if err != nil || d.D() < 0 {
			return v, fmt.Errorf("%q is not a duration", s)
		}
		v.Duration = d.D()
		return v, nil
	case UnitBytes:
		for _, suf := range byteSuffixes {
			if num, ok := strings.CutSuffix(s, suf.suffix); ok && suf.suffix != "B" {
				f, err := decimal(strings.TrimSpace(num))
				if err != nil {
					return v, fmt.Errorf("%q is not a byte size", s)
				}
				v.Number = f * suf.mult
				return v, nil
			}
		}
	}
	f, err := decimal(s)
	if err != nil {
		return v, err
	}
	if unit == UnitRatio && f > 1 {
		return v, fmt.Errorf("ratio %q is above 1", s)
	}
	v.Number = f
	return v, nil
}

var decimalPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// decimal reads a plain non-negative decimal number: no sign, hex,
// underscores, NaN or Inf, and finite.
func decimal(s string) (float64, error) {
	if !decimalPattern.MatchString(s) {
		return 0, fmt.Errorf("%q is not a finite non-negative number", s)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q is not a finite non-negative number", s)
	}
	return f, nil
}

// load parses every check file in fsys and returns them by code order.
func load(fsys fs.FS) ([]*Check, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []*Check
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		c, err := parse(e.Name(), b)
		if err != nil {
			return nil, err
		}
		if seen[c.Code] {
			return nil, fmt.Errorf("check %s defined twice", c.Code)
		}
		seen[c.Code] = true
		out = append(out, c)
	}
	return out, nil
}
