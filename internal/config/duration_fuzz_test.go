package config

import (
	"strings"
	"testing"
	"time"
)

// FuzzParseDuration checks the duration syntax of the configuration file
// and the flags: a value it accepts is never negative when written in days,
// a number of days is a whole number of days, and the Go form that
// MarshalText writes parses back to the same value.
func FuzzParseDuration(f *testing.F) {
	for _, s := range []string{
		"14d", "0d", "1d", "+3d", "-1d", "xd", "d", " 7d ", "1.5d", "30s", "336h", "1h30m",
		"-5m", "0", "", "1e3d", "106751d", "106752d", "9223372036854775807d", "9223372036854775808d",
		"2562047h47m16.854775807s", "2562047h47m16.854775808s", "1µs", "1us", "١d",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 64 {
			return
		}
		d, err := ParseDuration(s)
		again, againErr := ParseDuration(s)
		if d != again || (err == nil) != (againErr == nil) {
			t.Fatalf("ParseDuration(%q) is not deterministic", s)
		}
		if err != nil {
			if d != 0 {
				t.Fatalf("ParseDuration(%q) failed but returned %v", s, d)
			}
			if !strings.Contains(err.Error(), "invalid duration") {
				t.Fatalf("ParseDuration(%q) error %q does not say the duration is invalid", s, err)
			}
			return
		}
		if strings.HasSuffix(strings.TrimSpace(s), "d") {
			if d < 0 {
				t.Fatalf("ParseDuration(%q) = %v, a negative number of days", s, d.D())
			}
			if d.D()%(24*time.Hour) != 0 {
				t.Fatalf("ParseDuration(%q) = %v, not a whole number of days", s, d.D())
			}
		}
		text, _ := d.MarshalText()
		var back Duration
		if err := back.UnmarshalText(text); err != nil || back != d {
			t.Fatalf("ParseDuration(%q) = %v writes %q, which reads back as %v (%v)", s, d.D(), text, back.D(), err)
		}
	})
}
