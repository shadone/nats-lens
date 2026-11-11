package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A number of days too large for a time.Duration is an error. Multiplied
// without a bound, 213504 days wraps to 25 minutes and 106752 days to a
// negative duration.
func TestParseDurationDaysOverflow(t *testing.T) {
	for _, s := range []string{"106752d", "110000d", "213504d", "213505d", "9223372036854775807d"} {
		if d, err := ParseDuration(s); err == nil {
			t.Errorf("ParseDuration(%q) = %v, want an error", s, d.D())
		}
	}
	d, err := ParseDuration("106751d")
	if err != nil || d.D() != 106751*24*time.Hour {
		t.Fatalf("ParseDuration(106751d) = %v, %v; want the largest whole number of days", d.D(), err)
	}
}

func TestConfigRefusesTooManyDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "natslens.yaml")
	if err := os.WriteFile(path, []byte("retention: 213504d\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path, Default())
	if err == nil || !strings.Contains(err.Error(), `invalid duration "213504d"`) {
		t.Fatalf("LoadFile = %v, want the invalid duration named", err)
	}
}
