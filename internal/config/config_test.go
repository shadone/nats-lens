package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "natslens.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseDefaults(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval.D() != 30*time.Second || cfg.Retention.D() != 14*24*time.Hour {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestFileThenFlags(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	p := writeFile(t, `
interval: 10s
timeout: 2s
data_dir: /var/lib/natslens
retention: 7d
nats:
  url: nats://a:4222
  user: sys
  password: from-file
`)
	cfg, err := Parse("natslens", []string{"-config", p, "-interval", "20s", "-log-level", "debug"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval.D() != 20*time.Second {
		t.Errorf("flag should override file interval, got %s", cfg.Interval)
	}
	if cfg.Timeout.D() != 2*time.Second || cfg.DataDir != "/var/lib/natslens" {
		t.Errorf("file values lost: %+v", cfg)
	}
	if cfg.Retention.D() != 7*24*time.Hour {
		t.Errorf("retention = %s", cfg.Retention)
	}
	if cfg.NATS.Password != "from-file" || cfg.LogLevel != "debug" {
		t.Errorf("unexpected: %+v", cfg.NATS)
	}
}

func TestPasswordFromEnv(t *testing.T) {
	t.Setenv(PasswordEnv, "from-env")
	cfg, err := Parse("natslens", []string{"-nats-user", "sys"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NATS.Password != "from-env" {
		t.Fatalf("password not read from env")
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	p := writeFile(t, "intervall: 10s\n")
	if _, err := Parse("natslens", []string{"-config", p}); err == nil {
		t.Fatal("expected an error for an unknown key")
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]func(*Config){
		"timeout not below interval": func(c *Config) { c.Timeout = c.Interval },
		"three windows exceed interval": func(c *Config) {
			c.Interval, c.Timeout = Duration(10*time.Second), Duration(4*time.Second)
		},
		"creds and password": func(c *Config) { c.NATS.Creds = "x.creds"; c.NATS.Password = "p" },
		"cert without key":   func(c *Config) { c.NATS.TLS.Cert = "c.pem" },
		"bad log level":      func(c *Config) { c.LogLevel = "loud" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := Default()
			mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestLogValueHidesPassword(t *testing.T) {
	c := Default()
	c.NATS.Password = "s3cret-value"
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", c)
	if strings.Contains(buf.String(), "s3cret-value") {
		t.Fatalf("password leaked into log: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"nats_password_set":true`) {
		t.Fatalf("expected password_set marker: %s", buf.String())
	}
}

func TestParseDurationDays(t *testing.T) {
	d, err := ParseDuration("14d")
	if err != nil || d.D() != 336*time.Hour {
		t.Fatalf("got %v, %v", d, err)
	}
	if _, err := ParseDuration("xd"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLogValueRedactsURLPassword(t *testing.T) {
	c := Default()
	c.NATS.URL = "nats://sys:url-secret@a:4222,nats://b:4222"
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", c)
	if strings.Contains(buf.String(), "url-secret") {
		t.Fatalf("password leaked into log: %s", buf.String())
	}
}

func TestAccountsAndEvents(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	t.Setenv("NATSLENS_ACCOUNT_ORDER_API_PASSWORD", "acc-from-env")
	p := writeFile(t, `
data_dir: /var/lib/natslens
nats:
  url: nats://sys-host:4222
  tls:
    ca: /etc/ca.pem
    cert: /etc/sys.pem
    key: /etc/sys-key.pem
events:
  api_audit: true
accounts:
  - name: order-api
    user: app
  - name: OTHER
    url: nats://other:4222
    creds: /etc/other.creds
`)
	cfg, err := Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Events.Enabled || !cfg.Events.APIAudit || cfg.Events.BatchSize != 500 {
		t.Errorf("events = %+v", cfg.Events)
	}
	if cfg.ResolvedArchiveDir() != "/var/lib/natslens/archive" {
		t.Errorf("archive dir = %s", cfg.ResolvedArchiveDir())
	}
	if len(cfg.Accounts) != 2 || cfg.Accounts[0].Password != "acc-from-env" {
		t.Fatalf("accounts = %+v", cfg.Accounts)
	}
	conn := cfg.Accounts[0].Connection(cfg.NATS)
	if conn.URL != "nats://sys-host:4222" || conn.TLS.CA != "/etc/ca.pem" || conn.TLS.Cert != "" {
		t.Errorf("account connection should inherit URL and CA only: %+v", conn)
	}
	if conn.Name != "natslens-order-api" {
		t.Errorf("connection name = %s", conn.Name)
	}
	if got := cfg.Accounts[1].Connection(cfg.NATS).URL; got != "nats://other:4222" {
		t.Errorf("own URL lost: %s", got)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("cfg", "config", cfg)
	if strings.Contains(buf.String(), "acc-from-env") {
		t.Fatal("account password logged")
	}
}

func TestAccountValidation(t *testing.T) {
	for name, body := range map[string]string{
		"missing name":   "accounts: [ { user: a } ]",
		"duplicate name": "accounts: [ { name: A }, { name: A } ]",
		"creds and user": "accounts: [ { name: A, creds: /x, user: u } ]",
		"bad batch":      "events: { batch_size: 0 }",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("natslens", []string{"-config", writeFile(t, body+"\n")}); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestArchiveDirFlag(t *testing.T) {
	cfg, err := Parse("natslens", []string{"-archive-dir", "/srv/archive"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResolvedArchiveDir() != "/srv/archive" {
		t.Fatalf("archive dir = %s", cfg.ResolvedArchiveDir())
	}
}

// The password variable belongs to user/password login; with a creds file
// and no user it must not turn into a conflicting password.
func TestPasswordEnvIgnoredWithoutUser(t *testing.T) {
	t.Setenv(PasswordEnv, "from-env")
	cfg, err := Parse("natslens", []string{"-nats-creds", "sys.creds"})
	if err != nil {
		t.Fatalf("creds with the password variable set: %v", err)
	}
	if cfg.NATS.Password != "" {
		t.Fatal("password read without a user")
	}
}

func TestAccountPasswordEnvIgnoredWithoutUser(t *testing.T) {
	t.Setenv(AccountPasswordEnv("APP"), "from-env")
	p := writeFile(t, "accounts:\n  - name: APP\n    creds: app.creds\n")
	cfg, err := Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatalf("account creds with the password variable set: %v", err)
	}
	if cfg.Accounts[0].Password != "" {
		t.Fatal("account password read without a user")
	}
}

func TestForgetAfterAndConnzLimitFlags(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", []string{"-server-forget-after", "2h", "-connz-limit", "50"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerForgetAfter.D() != 2*time.Hour || cfg.ConnzLimit != 50 {
		t.Fatalf("got forget=%s connz=%d", cfg.ServerForgetAfter, cfg.ConnzLimit)
	}
	c := Default()
	c.ServerForgetAfter = 0
	if err := c.Validate(); err == nil {
		t.Fatal("server_forget_after 0 must be rejected")
	}
}

func TestArchiveAndFlushValidation(t *testing.T) {
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ArchiveProvisionalEvery.D() != 5*time.Minute {
		t.Errorf("archive_provisional_every default = %s", cfg.ArchiveProvisionalEvery)
	}
	for name, body := range map[string]string{
		"flush too long":       "events: { flush_interval: 10m }",
		"negative provisional": "archive_provisional_every: -1s",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("natslens", []string{"-config", writeFile(t, body+"\n")}); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestShutdownGrace(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ShutdownGrace.D() != 5*time.Second {
		t.Fatalf("default shutdown_grace = %s", cfg.ShutdownGrace)
	}
	p := writeFile(t, "shutdown_grace: 8s\n")
	if cfg, err = Parse("natslens", []string{"-config", p}); err != nil || cfg.ShutdownGrace.D() != 8*time.Second {
		t.Fatalf("file: %s %v", cfg.ShutdownGrace, err)
	}
	if cfg, err = Parse("natslens", []string{"-config", p, "-shutdown-grace", "2s"}); err != nil || cfg.ShutdownGrace.D() != 2*time.Second {
		t.Fatalf("flag: %s %v", cfg.ShutdownGrace, err)
	}
	if _, err := Parse("natslens", []string{"-shutdown-grace", "0s"}); err == nil {
		t.Fatal("shutdown_grace 0 must be rejected")
	}
}

// The epoch write is capped at interval, so a longer grace would promise
// time that is never given.
func TestShutdownGraceNotAboveInterval(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	if _, err := Parse("natslens", []string{"-interval", "30s", "-shutdown-grace", "40s"}); err == nil {
		t.Fatal("shutdown_grace above interval must be rejected")
	}
	if _, err := Parse("natslens", []string{"-interval", "30s", "-shutdown-grace", "30s"}); err != nil {
		t.Fatalf("shutdown_grace equal to interval: %v", err)
	}
}

func TestListen(t *testing.T) {
	cfg, err := Parse("natslens", nil)
	if err != nil || cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("default listen = %q, %v", cfg.Listen, err)
	}
	if cfg, err = Parse("natslens", []string{"-listen", ""}); err != nil || cfg.Listen != "" {
		t.Fatalf("-listen '' = %q, %v", cfg.Listen, err)
	}
	if _, err = Parse("natslens", []string{"-listen", ":9090"}); err == nil {
		t.Fatal("-listen :9090 without IAP was accepted")
	}
	if cfg, err = Parse("natslens", []string{"-listen", ":9090", "-iap-audience", "/projects/1/global/backendServices/2"}); err != nil || cfg.Listen != ":9090" {
		t.Fatalf("-listen :9090 = %q, %v", cfg.Listen, err)
	}
	if _, err := Parse("natslens", []string{"-listen", "localhost"}); err == nil {
		t.Fatal("a listen address without a port was accepted")
	}
}

func TestAllowedHostsAndIAPAudience(t *testing.T) {
	cfg, err := Parse("natslens", []string{"-allowed-host", "nats-lens.example.com, localhost,", "-iap-audience", "/projects/1/global/backendServices/2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedHosts) != 2 || cfg.AllowedHosts[0] != "nats-lens.example.com" || cfg.IAPAudience != "/projects/1/global/backendServices/2" {
		t.Fatalf("hosts %v, audience %q", cfg.AllowedHosts, cfg.IAPAudience)
	}
	if cfg, err = Parse("natslens", nil); err != nil || cfg.AllowedHosts != nil || cfg.IAPAudience != "" {
		t.Fatalf("defaults: %v %q %v", cfg.AllowedHosts, cfg.IAPAudience, err)
	}
}

// Threshold values are read as text whatever YAML type they have; the
// checks package parses them in the threshold's unit.
func TestChecksAndInventory(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	p := writeFile(t, `
inventory_file: /etc/natslens/inventory.yaml
checks:
  timeout: 2s
  disabled: [stream-005]
  thresholds:
    server-007: {warn_ratio: 0.8, crit_ratio: "0.95"}
    consumer-006: {max_idle: 10m}
    server-011: {min_new: 100}
`)
	cfg, err := Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatal(err)
	}
	th := cfg.Checks.Thresholds
	if cfg.InventoryFile != "/etc/natslens/inventory.yaml" || cfg.Checks.Timeout.D() != 2*time.Second ||
		len(cfg.Checks.Disabled) != 1 || th["server-007"]["warn_ratio"] != "0.8" || th["server-007"]["crit_ratio"] != "0.95" ||
		th["consumer-006"]["max_idle"] != "10m" || th["server-011"]["min_new"] != "100" {
		t.Fatalf("checks = %+v, inventory %q", cfg.Checks, cfg.InventoryFile)
	}
	cfg, err = Parse("natslens", []string{"-config", p, "-inventory-file", "other.yaml"})
	if err != nil || cfg.InventoryFile != "other.yaml" {
		t.Fatalf("flag: %q %v", cfg.InventoryFile, err)
	}
	bad := Default()
	bad.Checks.Timeout = Duration(time.Minute)
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "checks.timeout") {
		t.Fatalf("checks.timeout above interval: %v", err)
	}
}

func TestArchiveRetention(t *testing.T) {
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ArchiveRetention.D() != 90*24*time.Hour {
		t.Errorf("archive_retention default = %s", cfg.ArchiveRetention)
	}
	cfg, err = Parse("natslens", []string{"-config", writeFile(t, "archive_retention: 0\n")})
	if err != nil || cfg.ArchiveRetention.D() != 0 {
		t.Fatalf("archive_retention 0: %v %s", err, cfg.ArchiveRetention)
	}
	for name, body := range map[string]string{
		"negative":            "archive_retention: -1h",
		"shorter than retain": "retention: 14d\narchive_retention: 7d",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("natslens", []string{"-config", writeFile(t, body+"\n")}); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestChurnDefaultsAndOverride(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	ch := cfg.Events.Churn
	if !ch.Collapse || ch.PairWithin.D() != 10*time.Second || ch.Window.D() != time.Minute {
		t.Errorf("churn defaults = %+v", ch)
	}
	p := writeFile(t, "events:\n  churn:\n    collapse: false\n")
	if cfg, err = Parse("natslens", []string{"-config", p}); err != nil || cfg.Events.Churn.Collapse || cfg.Events.Churn.Window.D() != time.Minute {
		t.Errorf("collapse off: %+v %v", cfg.Events.Churn, err)
	}
	p = writeFile(t, "events:\n  churn:\n    pair_within: 2m\n    window: 1m\n")
	if _, err := Parse("natslens", []string{"-config", p}); err == nil {
		t.Error("pair_within longer than window accepted")
	}
}

// checks.remediation_file in the YAML file reaches config.Checks.
func TestLoadFileRemediationFile(t *testing.T) {
	p := writeFile(t, "checks:\n  remediation_file: /etc/natslens/remediation.yaml\n")
	cfg, err := LoadFile(p, Default())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Checks.RemediationFile != "/etc/natslens/remediation.yaml" {
		t.Fatalf("remediation_file = %q", cfg.Checks.RemediationFile)
	}
}
