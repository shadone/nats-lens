package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionsDefaults(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SubscriptionsInterval.D() != 5*time.Minute || cfg.SubszLimit != 10000 {
		t.Fatalf("defaults: interval %s, limit %d", cfg.SubscriptionsInterval, cfg.SubszLimit)
	}
}

func TestSubscriptionsFlagsAndFile(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	p := writeFile(t, "subscriptions_interval: 10m\nsubsz_limit: 500\n")
	cfg, err := Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SubscriptionsInterval.D() != 10*time.Minute || cfg.SubszLimit != 500 {
		t.Fatalf("file: interval %s, limit %d", cfg.SubscriptionsInterval, cfg.SubszLimit)
	}
	cfg, err = Parse("natslens", []string{"-config", p, "-subscriptions-interval", "0", "-subsz-limit", "20"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SubscriptionsInterval.D() != 0 || cfg.SubszLimit != 20 {
		t.Fatalf("flags: interval %s, limit %d", cfg.SubscriptionsInterval, cfg.SubszLimit)
	}
}

func TestSubscriptionsValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(c *Config)
		want string
	}{
		"negative interval": {func(c *Config) { c.SubscriptionsInterval = Duration(-time.Second) }, "subscriptions_interval must not be negative"},
		"zero limit":        {func(c *Config) { c.SubszLimit = 0 }, "subsz_limit must be positive"},
		"negative limit":    {func(c *Config) { c.SubszLimit = -1 }, "subsz_limit must be positive"},
		"limit too large":   {func(c *Config) { c.SubszLimit = MaxSubszLimit + 1 }, "subsz_limit must be at most 100000: every server's list"},
	} {
		t.Run(name, func(t *testing.T) {
			c := Default()
			tc.set(&c)
			err := c.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not say %q", err, tc.want)
			}
		})
	}
	c := Default()
	c.SubscriptionsInterval = 0
	if err := c.Validate(); err != nil {
		t.Fatalf("interval 0 turns collections off and is valid: %v", err)
	}
	c.SubszLimit = MaxSubszLimit
	if err := c.Validate(); err != nil {
		t.Fatalf("subsz_limit %d is valid: %v", MaxSubszLimit, err)
	}
}

// The start log names both settings.
func TestSubscriptionsInStartLog(t *testing.T) {
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("starting", "config", Default())
	for _, want := range []string{"config.subscriptions_interval=5m0s", "config.subsz_limit=10000"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("start log lacks %s: %s", want, buf.String())
		}
	}
}
