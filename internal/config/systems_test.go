package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// withCgroupLimit points the memory budget at a cgroup file of the given
// limit for the length of the test.
func withCgroupLimit(t *testing.T, limit string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "memory.max")
	if err := os.WriteFile(p, []byte(limit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := cgroupMemoryFiles
	cgroupMemoryFiles = []string{p}
	t.Cleanup(func() { cgroupMemoryFiles = old })
}

func parseFile(t *testing.T, body string, args ...string) Config {
	t.Helper()
	cfg, err := Parse("natslens", append([]string{"-config", writeFile(t, body)}, args...))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLegacyConfigResolvesToOneDefaultSystem(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg := parseFile(t, "data_dir: /var/lib/natslens\n")
	sys := cfg.ResolvedSystems()
	if len(sys) != 1 {
		t.Fatalf("got %d systems", len(sys))
	}
	if sys[0].ID != DefaultSystemID || sys[0].Name != DefaultSystemID {
		t.Errorf("id %q name %q", sys[0].ID, sys[0].Name)
	}
	if sys[0].DataDir != "/var/lib/natslens" {
		t.Errorf("data dir %q, want data_dir unchanged", sys[0].DataDir)
	}
	if sys[0].ArchiveDir != "/var/lib/natslens/archive" {
		t.Errorf("archive dir %q", sys[0].ArchiveDir)
	}
}

func TestLegacyConfigTakesSystemID(t *testing.T) {
	cfg := parseFile(t, "system_id: shop-prod\n")
	if got := cfg.ResolvedSystems()[0].ID; got != "shop-prod" {
		t.Fatalf("id %q", got)
	}
}

// TestLegacyResolvedSystemMatchesSingleSystemFields proves that a config
// without systems resolves to exactly the values natslens reads from the
// single-system fields, so the runtime can switch to ResolvedSystems
// without a change in behaviour.
func TestLegacyResolvedSystemMatchesSingleSystemFields(t *testing.T) {
	withCgroupLimit(t, "2147483648")
	t.Setenv(PasswordEnv, "sys-secret")
	t.Setenv(AccountPasswordEnv("APP"), "app-secret")
	cfg := parseFile(t, `
data_dir: /var/lib/natslens
archive_dir: /mnt/archive
retention: 7d
archive_retention: 30d
nats:
  url: nats://a:4222,nats://b:4222
  name: lens
  user: sys
  tls: {ca: /etc/ca.pem, cert: /etc/c.pem, key: /etc/k.pem}
accounts:
  - name: APP
    user: app
  - name: OTHER
    url: nats://other:4222
    creds: /etc/other.creds
inventory_file: /etc/inventory.yaml
kubernetes:
  enabled: true
  kubeconfig: /etc/kubeconfig
  context: prod
  namespaces: [nats]
  nats_selector: app=nats
  server_name: "{pod}.{namespace}"
  nodes: false
  client_pods: '^resize-worker'
`)
	sys := cfg.ResolvedSystems()
	if len(sys) != 1 {
		t.Fatalf("got %d systems", len(sys))
	}
	s := sys[0]
	check := func(field string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %#v, want %#v", field, got, want)
		}
	}
	check("nats", s.NATS, cfg.NATS)
	check("nats password from env", s.NATS.Password, "sys-secret")
	check("accounts", s.Accounts, cfg.Accounts)
	check("account password from env", s.Accounts[0].Password, "app-secret")
	check("inventory_file", s.InventoryFile, cfg.InventoryFile)
	check("kubernetes", s.Kubernetes, cfg.Kubernetes)
	check("retention", s.Retention, cfg.Retention)
	check("archive_retention", s.ArchiveRetention, cfg.ArchiveRetention)
	check("data_dir", s.DataDir, cfg.DataDir)
	check("archive dir", s.ArchiveDir, cfg.ResolvedArchiveDir())
	check("duckdb memory limit", s.DuckDBMemoryLimit, cfg.DuckDBMemoryLimit())
	check("tooling names", s.ToolingNames(), cfg.ToolingNames())
	check("tooling names value", s.ToolingNames(), []string{"lens", "lens-events", "lens-APP", "lens-OTHER"})
}

const twoSystems = `
data_dir: /var/lib/natslens
retention: 10d
duckdb_memory_limit: 3GiB
systems:
  - id: prod
    name: Production
    environment: production
    nats: {url: "nats://prod:4222", user: sys}
    accounts:
      - name: APP
        user: app
    kubernetes:
      enabled: true
      namespaces: [nats]
    retention: 20d
    archive_retention: 0d
    duckdb_memory_limit: 512MiB
  - id: eu-west
    nats: {url: "nats://eu:4222", creds: /etc/eu.creds}
    data_dir: /data/eu
`

func TestSystemsListTakesDefaults(t *testing.T) {
	t.Setenv(PasswordEnv, "legacy")
	cfg := parseFile(t, twoSystems)
	sys := cfg.ResolvedSystems()
	if len(sys) != 2 {
		t.Fatalf("got %d systems", len(sys))
	}
	prod, eu := sys[0], sys[1]
	if prod.ID != "prod" || prod.Name != "Production" || prod.Environment != "production" {
		t.Errorf("prod identity: %+v", prod)
	}
	if eu.ID != "eu-west" || eu.Name != "eu-west" {
		t.Errorf("eu name should default to the id: %q", eu.Name)
	}
	if prod.DataDir != "/var/lib/natslens/prod" || prod.ArchiveDir != "/var/lib/natslens/prod/archive" {
		t.Errorf("prod dirs %q %q", prod.DataDir, prod.ArchiveDir)
	}
	if eu.DataDir != "/data/eu" || eu.ArchiveDir != "/data/eu/archive" {
		t.Errorf("eu dirs %q %q", eu.DataDir, eu.ArchiveDir)
	}
	if prod.Retention.D() != 20*24*time.Hour || prod.ArchiveRetention != 0 {
		t.Errorf("prod retention %s / %s", prod.Retention, prod.ArchiveRetention)
	}
	if eu.Retention.D() != 10*24*time.Hour || eu.ArchiveRetention.D() != 90*24*time.Hour {
		t.Errorf("eu retention should default to the top level: %s / %s", eu.Retention, eu.ArchiveRetention)
	}
	if prod.NATS.Name != "natslens-prod" || eu.NATS.Name != "natslens-eu-west" {
		t.Errorf("nats name defaults: %q %q", prod.NATS.Name, eu.NATS.Name)
	}
	if prod.NATS.Password != "" {
		t.Errorf("the single-system password variable must not apply to a system")
	}
	k := prod.Kubernetes
	if !k.Enabled || k.NATSSelector != DefaultNATSSelector || !k.Nodes || k.ServerName != "{pod}" || k.GapAfter.D() != 30*time.Second {
		t.Errorf("kubernetes defaults lost: %+v", k)
	}
	if eu.Kubernetes.Enabled || eu.Kubernetes.NATSSelector != DefaultNATSSelector {
		t.Errorf("eu kubernetes: %+v", eu.Kubernetes)
	}
	if prod.DuckDBMemoryLimit != 512<<20 {
		t.Errorf("prod memory %d", prod.DuckDBMemoryLimit)
	}
	if eu.DuckDBMemoryLimit != 3<<30-512<<20 {
		t.Errorf("eu memory %d, want the budget left after prod's own limit", eu.DuckDBMemoryLimit)
	}
	if got := eu.ToolingNames(); !reflect.DeepEqual(got, []string{"natslens-eu-west", "natslens-eu-west-events"}) {
		t.Errorf("eu tooling names %v", got)
	}
}

func TestSystemsArchiveRoot(t *testing.T) {
	cfg := parseFile(t, `
data_dir: /var/lib/natslens
archive_dir: /mnt/archive
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: b, nats: {url: "nats://b:4222"}, archive_dir: /mnt/b}
`)
	sys := cfg.ResolvedSystems()
	if sys[0].ArchiveDir != "/mnt/archive/a" || sys[1].ArchiveDir != "/mnt/b" {
		t.Fatalf("archive dirs %q %q", sys[0].ArchiveDir, sys[1].ArchiveDir)
	}
}

func TestSystemsPasswordsFromEnv(t *testing.T) {
	t.Setenv("NATSLENS_SYSTEM_PROD_NATS_PASSWORD", "prod-sys")
	t.Setenv("NATSLENS_SYSTEM_PROD_ACCOUNT_APP_PASSWORD", "prod-app")
	t.Setenv(AccountPasswordEnv("APP"), "legacy-app")
	cfg := parseFile(t, twoSystems)
	prod := cfg.ResolvedSystems()[0]
	if prod.NATS.Password != "prod-sys" {
		t.Errorf("system password %q", prod.NATS.Password)
	}
	if prod.Accounts[0].Password != "prod-app" {
		t.Errorf("account password %q", prod.Accounts[0].Password)
	}
	if got := SystemPasswordEnv("eu-west"); got != "NATSLENS_SYSTEM_EU_WEST_NATS_PASSWORD" {
		t.Errorf("env name %q", got)
	}
	if got := SystemAccountPasswordEnv("eu-west", "app.x"); got != "NATSLENS_SYSTEM_EU_WEST_ACCOUNT_APP_X_PASSWORD" {
		t.Errorf("env name %q", got)
	}
}

func TestSystemsRefused(t *testing.T) {
	sys := func(extra string) string {
		return "systems:\n  - {id: a, nats: {url: \"nats://a:4222\"}}\n" + extra
	}
	cases := []struct {
		name, body, want string
		args             []string
	}{
		{"top-level nats", "nats: {url: \"nats://x:4222\"}\n" + sys(""), "nats", nil},
		{"top-level accounts", "accounts: []\n" + sys(""), "accounts", nil},
		{"top-level inventory", "inventory_file: /x\n" + sys(""), "inventory_file", nil},
		{"top-level kubernetes", "kubernetes: {enabled: false}\n" + sys(""), "kubernetes", nil},
		{"system_id with systems", "system_id: x\n" + sys(""), "system_id", nil},
		{"nats-url flag", sys(""), "-nats-url", []string{"-nats-url", "nats://y:4222"}},
		{"inventory-file flag", sys(""), "-inventory-file", []string{"-inventory-file", "/x"}},
		{"duplicate id", sys("  - {id: a, nats: {url: \"nats://b:4222\"}}\n"), "duplicate", nil},
		{"bad id", "systems:\n  - {id: Prod.1, nats: {url: \"nats://a:4222\"}}\n", "Prod.1", nil},
		{"empty id", "systems:\n  - {nats: {url: \"nats://a:4222\"}}\n", "id", nil},
		{"bad legacy system_id", "system_id: Prod.1\n", "Prod.1", nil},
		{"missing url", "systems:\n  - {id: a}\n", "nats.url", nil},
		{"creds and user", "systems:\n  - {id: a, nats: {url: \"nats://a:4222\", creds: /c, user: u}}\n", "mutually exclusive", nil},
		{"same data dir", sys("  - {id: b, nats: {url: \"nats://b:4222\"}, data_dir: ./data/a}\n"), "same directory", nil},
		{"same archive dir", "archive_dir: /arch\n" + sys("  - {id: b, nats: {url: \"nats://b:4222\"}, archive_dir: /arch/a}\n"), "same directory", nil},
		{"archive retention below retention", "systems:\n  - {id: a, nats: {url: \"nats://a:4222\"}, retention: 30d, archive_retention: 7d}\n", "archive_retention", nil},
		{"kubernetes without events", "events: {enabled: false}\nsystems:\n  - {id: a, nats: {url: \"nats://a:4222\"}, kubernetes: {enabled: true}}\n", "events.enabled", nil},
		{"duplicate account", "systems:\n  - {id: a, nats: {url: \"nats://a:4222\"}, accounts: [{name: APP}, {name: APP}]}\n", "duplicate name", nil},
		{"unknown key in a system", "systems:\n  - {id: a, nats: {url: \"nats://a:4222\"}, retension: 7d}\n", "retension", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"-config", writeFile(t, tc.body)}, tc.args...)
			_, err := Parse("natslens", args)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestSystemsLoadFileRefusesTopLevelNATS(t *testing.T) {
	p := writeFile(t, "nats: {url: \"nats://x:4222\"}\nsystems:\n  - {id: a, nats: {url: \"nats://a:4222\"}}\n")
	if _, err := LoadFile(p, Default()); err == nil {
		t.Fatal("LoadFile accepted top-level nats with systems")
	}
}

func TestSystemsDataDirFlagSetsRoot(t *testing.T) {
	cfg := parseFile(t, twoSystems, "-data-dir", "/srv/lens")
	if got := cfg.ResolvedSystems()[0].DataDir; got != "/srv/lens/prod" {
		t.Fatalf("data dir %q", got)
	}
}

func TestSystemsLogValueHidesPasswords(t *testing.T) {
	t.Setenv("NATSLENS_SYSTEM_PROD_NATS_PASSWORD", "prod-sys")
	cfg := parseFile(t, twoSystems)
	if s := cfg.LogValue().String(); strings.Contains(s, "prod-sys") || !strings.Contains(s, "eu-west") {
		t.Fatalf("log value %s", s)
	}
}
