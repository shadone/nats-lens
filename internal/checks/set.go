package checks

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	checkfiles "github.com/shadone/nats-lens/checks"
	"github.com/shadone/nats-lens/internal/config"
)

// CheckFailedCode is the engine's own check: one or more checks failed to
// run in the epoch.
const CheckFailedCode = "system-001"

// checkFailed is the header of the engine's own check; it has no query.
func checkFailed() *Check {
	return &Check{
		Code: CheckFailedCode, Name: "Check failed to run", Entity: "system", Category: "engine",
		Severity: "warning", Kind: "operational",
		Description: "One or more checks returned an error or ran out of time in the epoch. " +
			"Their findings keep the state of the last run that worked, so a problem they look for can go unreported.",
		Remediation: "Read the error in the detail and in the natslens log (`check failed`). A timeout means the query " +
			"took longer than `checks.timeout` (default 5s): look for a table that grew unusually (connections, events) or raise the timeout. " +
			"An SQL error after an upgrade means a check file and the schema disagree; run `natslens checks run -data-dir DIR` " +
			"against a copy of the database to see the error.",
		builtin: true,
	}
}

// Set is the loaded checks with the configured overrides applied.
type Set struct {
	checks   []*Check
	byCode   map[string]*Check
	disabled map[string]bool
	values   map[string]map[string]Value
	lookback map[string]time.Duration
	timeout  time.Duration
	restart  time.Duration
	// tooling are nats-lens's own connection names; includeTooling makes
	// the client checks count tooling connections.
	tooling         []string
	includeTooling  bool
	loopbackTooling bool
	// remediationExtended counts the checks checks.remediation_file added to.
	remediationExtended int
}

// SetToolingNames sets nats-lens's own connection names, which the client
// checks leave out unless checks.include_tooling is set.
func (s *Set) SetToolingNames(names []string) { s.tooling = append([]string(nil), names...) }

// Load reads the embedded check files and applies cfg. An unknown code in
// checks.disabled, checks.thresholds or the remediation file, an unknown
// threshold name or a value that does not parse is an error.
func Load(cfg config.Checks) (*Set, error) {
	return LoadFS(checkfiles.FS, cfg)
}

// LookbackKey is the threshold key that overrides a check's lookback.
const LookbackKey = "lookback"

// LoadFS is Load for the check files in fsys.
func LoadFS(fsys fs.FS, cfg config.Checks) (*Set, error) {
	files, err := load(fsys)
	if err != nil {
		return nil, err
	}
	all := append([]*Check{checkFailed()}, files...)
	sort.Slice(all, func(i, j int) bool { return all[i].Code < all[j].Code })
	s := &Set{
		checks: all, byCode: map[string]*Check{}, disabled: map[string]bool{},
		values: map[string]map[string]Value{}, lookback: map[string]time.Duration{}, timeout: cfg.Timeout.D(),
	}
	if s.timeout <= 0 {
		s.timeout = 5 * time.Second
	}
	s.includeTooling, s.loopbackTooling = cfg.IncludeTooling, cfg.LoopbackTooling()
	s.restart = cfg.RestartWindow.D()
	if s.restart <= 0 {
		s.restart = DefaultRestartWindow
	}
	for _, c := range all {
		if s.byCode[c.Code] != nil {
			return nil, fmt.Errorf("check %s defined twice", c.Code)
		}
		s.byCode[c.Code] = c
		s.lookback[c.Code] = c.lookback
		vals := map[string]Value{}
		for _, t := range c.Thresholds {
			v, err := parseValue(t.Unit, t.Default)
			if err != nil {
				return nil, fmt.Errorf("%s threshold %s: %w", c.Code, t.Name, err)
			}
			vals[t.Name] = v
		}
		s.values[c.Code] = vals
	}
	var errs []error
	for _, c := range all {
		for _, code := range c.SuppressedBy {
			other := s.byCode[code]
			switch {
			case other == nil:
				errs = append(errs, fmt.Errorf("%s: suppressed_by names unknown check %q", c.Code, code))
			case other == c || other.Entity != c.Entity:
				errs = append(errs, fmt.Errorf("%s: suppressed_by %s must be another check of entity %s", c.Code, code, c.Entity))
			}
		}
	}
	for _, code := range cfg.Disabled {
		if s.byCode[code] == nil {
			errs = append(errs, fmt.Errorf("checks.disabled: unknown check %q", code))
			continue
		}
		s.disabled[code] = true
	}
	for code, overrides := range cfg.Thresholds {
		c := s.byCode[code]
		if c == nil {
			errs = append(errs, fmt.Errorf("checks.thresholds: unknown check %q", code))
			continue
		}
		for name, raw := range overrides {
			if name == LookbackKey {
				if c.lookback == 0 {
					errs = append(errs, fmt.Errorf("checks.thresholds.%s.%s: the check has no lookback", code, name))
					continue
				}
				v, err := parseValue(UnitDuration, raw)
				if err != nil || v.Duration <= 0 {
					errs = append(errs, fmt.Errorf("checks.thresholds.%s.%s: %q is not a positive duration", code, name, raw))
					continue
				}
				s.lookback[code] = v.Duration
				continue
			}
			t, ok := c.threshold(name)
			if !ok {
				errs = append(errs, fmt.Errorf("checks.thresholds.%s: unknown threshold %q", code, name))
				continue
			}
			v, err := parseValue(t.Unit, raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("checks.thresholds.%s.%s: %w", code, name, err))
				continue
			}
			s.values[code][name] = v
		}
	}
	if cfg.RemediationFile != "" {
		extra, err := loadRemediation(cfg.RemediationFile)
		if err != nil {
			errs = append(errs, err)
		} else {
			errs = append(errs, s.addRemediation(cfg.RemediationFile, extra)...)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

func (c *Check) threshold(name string) (Threshold, bool) {
	for _, t := range c.Thresholds {
		if t.Name == name {
			return t, true
		}
	}
	return Threshold{}, false
}

// Checks returns every check, the engine's own included, by code.
func (s *Set) Checks() []*Check { return s.checks }

// Lookup returns the check with code.
func (s *Set) Lookup(code string) (*Check, bool) {
	c, ok := s.byCode[code]
	return c, ok
}

// Disabled reports whether the configuration disables the check.
func (s *Set) Disabled(code string) bool { return s.disabled[code] }

// Lookback returns the check's window after overrides; zero when it has
// none.
func (s *Set) Lookback(code string) time.Duration { return s.lookback[code] }

// Value returns a threshold's value after overrides.
func (s *Set) Value(code, name string) (Value, bool) {
	v, ok := s.values[code][name]
	return v, ok
}

// DefaultRestartWindow is how long after a server process started it
// counts as restarting, when checks.restart_window is not set.
const DefaultRestartWindow = 10 * time.Minute

// RestartWindow is how long a restarted server counts as restarting.
func (s *Set) RestartWindow() time.Duration { return s.restart }

// Timeout is how long one check's query may run.
func (s *Set) Timeout() time.Duration { return s.timeout }
