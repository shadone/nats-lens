package config

import (
	"reflect"
	"testing"
)

// The view of a system resolves back to the same system: every per-system
// setting reaches the code that reads a single-system configuration.
func TestForSystemResolvesBackToTheSystem(t *testing.T) {
	cfg := parseFile(t, `data_dir: /var/lib/natslens
retention: 7d
duckdb_memory_limit: 2GiB
systems:
  - id: shop-prod
    name: Production
    environment: production
    nats: { url: "nats://prod:4222", creds: /creds/prod }
    accounts: [{ name: APP, user: app, password: p }]
    inventory_file: /etc/natslens/prod.yaml
    retention: 21d
    archive_retention: 60d
    duckdb_memory_limit: 512MiB
  - id: shop-staging
    nats: { url: "nats://staging:4222" }
`)
	for _, sys := range cfg.ResolvedSystems() {
		view := cfg.ForSystem(sys)
		if len(view.Systems) != 0 {
			t.Fatalf("%s: the view still lists systems", sys.ID)
		}
		got := view.ResolvedSystems()
		if len(got) != 1 {
			t.Fatalf("%s: view resolves to %d systems", sys.ID, len(got))
		}
		back := got[0]
		// A single-system configuration has no display name or
		// environment of its own; those stay with the system.
		back.Name, back.Environment = sys.Name, sys.Environment
		if !reflect.DeepEqual(back, sys) {
			t.Errorf("%s: view resolves to\n%+v\nwant\n%+v", sys.ID, back, sys)
		}
		if view.Interval != cfg.Interval || view.Listen != cfg.Listen || !reflect.DeepEqual(view.Events, cfg.Events) {
			t.Errorf("%s: global settings changed in the view", sys.ID)
		}
	}
}

// A single-system configuration is its own view.
func TestForSystemOfLegacyConfig(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg := parseFile(t, "data_dir: /var/lib/natslens\nsystem_id: shop-prod\n")
	view := cfg.ForSystem(cfg.ResolvedSystems()[0])
	if view.DataDir != cfg.DataDir || view.ResolvedArchiveDir() != cfg.ResolvedArchiveDir() ||
		view.DuckDBMemoryLimit() != cfg.DuckDBMemoryLimit() || !reflect.DeepEqual(view.NATS, cfg.NATS) ||
		view.SystemID != "shop-prod" {
		t.Errorf("view %+v differs from the configuration %+v", view, cfg)
	}
}
