package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Duration is a time.Duration that also accepts a whole number of days
// with a "d" suffix ("14d"), as retention periods are usually given in days.
type Duration time.Duration

// ParseDuration parses Go duration syntax ("30s", "336h") or whole days ("14d").
func ParseDuration(s string) (Duration, error) {
	s = strings.TrimSpace(s)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return Duration(time.Duration(n) * 24 * time.Hour), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return Duration(d), nil
}

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalText lets YAML and flag parsing read a Duration from a string.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalText writes the Go duration form.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Set implements flag.Value.
func (d *Duration) Set(s string) error { return d.UnmarshalText([]byte(s)) }
