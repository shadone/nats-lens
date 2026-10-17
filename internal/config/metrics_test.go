package config

import (
	"testing"
	"time"
)

func TestMetricsTimeout(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricsTimeout.D() != 8*time.Second {
		t.Fatalf("default metrics_timeout = %s", cfg.MetricsTimeout)
	}
	p := writeFile(t, "metrics_timeout: 5s\n")
	if cfg, err = Parse("natslens", []string{"-config", p}); err != nil || cfg.MetricsTimeout.D() != 5*time.Second {
		t.Fatalf("file: %s %v", cfg.MetricsTimeout, err)
	}
	if _, err := Parse("natslens", []string{"-config", writeFile(t, "metrics_timeout: 0s\n")}); err == nil {
		t.Fatal("metrics_timeout 0 must be rejected")
	}
}
