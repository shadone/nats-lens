package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

// DefaultSystemID is the id of the one system of a configuration without
// systems when system_id is not set.
const DefaultSystemID = "default"

// DefaultNATSName is the connection name the servers show for nats-lens
// when nats.name is not set.
const DefaultNATSName = "natslens"

// systemID is the form of a system id: it appears in URLs, the API, metric
// labels and directory names.
var systemID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// SystemConfig is one entry of systems in the file. Fields left out take
// their defaults when the configuration is resolved.
type SystemConfig struct {
	// ID identifies the system in URLs, the API, metric labels and
	// directory names.
	ID string `yaml:"id"`
	// Name is the display name; empty means the id.
	Name string `yaml:"name"`
	// Environment is free text shown with the system.
	Environment string `yaml:"environment"`
	// NATS is the system account connection; nats.name defaults to natslens.
	NATS NATS `yaml:"nats"`
	// Accounts are the JetStream accounts whose advisories are captured.
	Accounts []Account `yaml:"accounts"`
	// InventoryFile lists the expected streams, consumers and buckets.
	InventoryFile string `yaml:"inventory_file"`
	// Kubernetes configures the system's Kubernetes watcher.
	Kubernetes Kubernetes `yaml:"kubernetes"`
	// Retention is how long epochs stay in DuckDB; nil means the top-level
	// retention.
	Retention *Duration `yaml:"retention"`
	// ArchiveRetention is how long archived hours are kept; nil means the
	// top-level archive_retention.
	ArchiveRetention *Duration `yaml:"archive_retention"`
	// DataDir holds the system's DuckDB file; empty means <data_dir>/<id>.
	DataDir string `yaml:"data_dir"`
	// ArchiveDir receives the system's hourly Parquet files; empty means
	// <archive_dir>/<id> when archive_dir is set, else <system data dir>/archive.
	ArchiveDir string `yaml:"archive_dir"`
	// DuckDBMemory bounds the memory of the system's DuckDB; zero means the
	// top-level budget divided by the number of systems.
	DuckDBMemory ByteSize `yaml:"duckdb_memory_limit"`
}

// UnmarshalYAML starts each system from its defaults, so that a key left
// out keeps its default rather than its zero value. It uses the decoder's
// own unmarshal function, so unknown keys are still refused.
func (s *SystemConfig) UnmarshalYAML(unmarshal func(any) error) error {
	type plain SystemConfig
	p := plain{NATS: NATS{Name: DefaultNATSName}, Kubernetes: defaultKubernetes()}
	if err := unmarshal(&p); err != nil {
		return err
	}
	*s = SystemConfig(p)
	return nil
}

// System is one monitored NATS system with every default applied.
type System struct {
	ID                string
	Name              string
	Environment       string
	NATS              NATS
	Accounts          []Account
	InventoryFile     string
	Kubernetes        Kubernetes
	Retention         Duration
	ArchiveRetention  Duration
	DataDir           string
	ArchiveDir        string
	DuckDBMemoryLimit int64
}

// ToolingNames are the connection names the system's nats-lens
// connections use: the collector's, its event connection's and each
// account connection's.
func (s System) ToolingNames() []string {
	out := []string{s.NATS.Name, s.NATS.Name + "-events"}
	for _, a := range s.Accounts {
		out = append(out, a.Connection(s.NATS).Name)
	}
	return out
}

// ResolvedSystems returns the monitored systems with every default
// applied. A configuration without systems is one system whose id is
// system_id (default "default") and whose directories are exactly data_dir
// and the archive directory a single-system configuration uses, so no file
// moves.
func (c Config) ResolvedSystems() []System {
	if len(c.Systems) == 0 {
		return []System{c.legacySystem()}
	}
	budget := c.DuckDBMemoryLimit()
	out := make([]System, 0, len(c.Systems))
	for _, s := range c.Systems {
		out = append(out, c.resolve(s, budget/int64(len(c.Systems))))
	}
	return out
}

func (c Config) legacySystem() System {
	id := c.SystemID
	if id == "" {
		id = DefaultSystemID
	}
	return System{
		ID:                id,
		Name:              id,
		NATS:              c.NATS,
		Accounts:          slices.Clone(c.Accounts),
		InventoryFile:     c.InventoryFile,
		Kubernetes:        c.Kubernetes,
		Retention:         c.Retention,
		ArchiveRetention:  c.ArchiveRetention,
		DataDir:           c.DataDir,
		ArchiveDir:        c.ResolvedArchiveDir(),
		DuckDBMemoryLimit: c.DuckDBMemoryLimit(),
	}
}

func (c Config) resolve(s SystemConfig, memoryShare int64) System {
	out := System{
		ID:                s.ID,
		Name:              s.Name,
		Environment:       s.Environment,
		NATS:              s.NATS,
		Accounts:          slices.Clone(s.Accounts),
		InventoryFile:     s.InventoryFile,
		Kubernetes:        s.Kubernetes,
		Retention:         c.Retention,
		ArchiveRetention:  c.ArchiveRetention,
		DataDir:           s.DataDir,
		ArchiveDir:        s.ArchiveDir,
		DuckDBMemoryLimit: memoryShare,
	}
	if out.Name == "" {
		out.Name = s.ID
	}
	if s.Retention != nil {
		out.Retention = *s.Retention
	}
	if s.ArchiveRetention != nil {
		out.ArchiveRetention = *s.ArchiveRetention
	}
	if out.DataDir == "" {
		out.DataDir = filepath.Join(c.DataDir, s.ID)
	}
	if out.ArchiveDir == "" {
		if c.ArchiveDir != "" {
			out.ArchiveDir = filepath.Join(c.ArchiveDir, s.ID)
		} else {
			out.ArchiveDir = filepath.Join(out.DataDir, "archive")
		}
	}
	if s.DuckDBMemory > 0 {
		out.DuckDBMemoryLimit = int64(s.DuckDBMemory)
	}
	return out
}

// SystemPasswordEnv returns the environment variable read for a listed
// system's NATS password: NATSLENS_SYSTEM_<ID>_NATS_PASSWORD, with the id
// upper-cased and every character other than A-Z and 0-9 replaced by an
// underscore.
func SystemPasswordEnv(id string) string {
	return "NATSLENS_SYSTEM_" + envName(id) + "_NATS_PASSWORD"
}

// SystemAccountPasswordEnv returns the environment variable read for an
// account password of a listed system:
// NATSLENS_SYSTEM_<ID>_ACCOUNT_<NAME>_PASSWORD.
func SystemAccountPasswordEnv(id, account string) string {
	return "NATSLENS_SYSTEM_" + envName(id) + "_ACCOUNT_" + envName(account) + "_PASSWORD"
}

// fillSystemPasswords reads each listed system's passwords from their
// environment variables where the file sets a user but no password.
func fillSystemPasswords(systems []SystemConfig) []SystemConfig {
	if len(systems) == 0 {
		return systems
	}
	out := make([]SystemConfig, len(systems))
	for i, s := range systems {
		if s.NATS.Password == "" && s.NATS.User != "" {
			s.NATS.Password = os.Getenv(SystemPasswordEnv(s.ID))
		}
		accs := make([]Account, len(s.Accounts))
		for j, a := range s.Accounts {
			if a.Password == "" && a.User != "" {
				a.Password = os.Getenv(SystemAccountPasswordEnv(s.ID, a.Name))
			}
			accs[j] = a
		}
		s.Accounts = accs
		out[i] = s
	}
	return out
}

// perSystemKeys are the top-level keys that describe one system; a
// configuration with systems sets them on each system instead.
var perSystemKeys = []string{"nats", "accounts", "inventory_file", "kubernetes", "system_id"}

// perSystemFlags are the flags that set a per-system key.
var perSystemFlags = []string{"nats-url", "nats-creds", "nats-user", "nats-tls-ca", "nats-tls-cert", "nats-tls-key", "inventory-file"}

// refusePerSystemKeys refuses top-level keys that a configuration with
// systems sets on each system.
func refusePerSystemKeys(keys map[string]bool) error {
	if !keys["systems"] {
		return nil
	}
	var errs []error
	for _, k := range perSystemKeys {
		if keys[k] {
			errs = append(errs, fmt.Errorf("top-level %s cannot be used with systems: set it on each system", k))
		}
	}
	return errors.Join(errs...)
}

// refusePerSystemFlags refuses flags that set a per-system key when the
// file lists systems.
func refusePerSystemFlags(cfg Config, set map[string]bool) error {
	if len(cfg.Systems) == 0 {
		return nil
	}
	var errs []error
	for _, f := range perSystemFlags {
		if set[f] {
			errs = append(errs, fmt.Errorf("-%s cannot be used with systems: set it on each system in the file", f))
		}
	}
	return errors.Join(errs...)
}

// validateSystems checks the system ids and each listed system's settings,
// and refuses two systems sharing a directory.
func (c Config) validateSystems() []error {
	if len(c.Systems) == 0 {
		if c.SystemID != "" && !systemID.MatchString(c.SystemID) {
			return []error{fmt.Errorf("system_id %q must match %s", c.SystemID, systemID)}
		}
		return nil
	}
	var errs []error
	if c.SystemID != "" {
		errs = append(errs, errors.New("system_id cannot be used with systems: give each system its id"))
	}
	seen := map[string]bool{}
	for i, s := range c.Systems {
		p := fmt.Sprintf("systems[%d]", i)
		switch {
		case !systemID.MatchString(s.ID):
			errs = append(errs, fmt.Errorf("%s: id %q must match %s", p, s.ID, systemID))
		case seen[s.ID]:
			errs = append(errs, fmt.Errorf("%s: duplicate id %q", p, s.ID))
		}
		seen[s.ID] = true
	}
	resolved := c.ResolvedSystems()
	for i, s := range resolved {
		errs = append(errs, prefixErrors(fmt.Sprintf("systems[%d]", i), s.validate(c.Events))...)
	}
	return append(errs, validateSystemDirs(resolved)...)
}

func (s System) validate(events Events) []error {
	var errs []error
	if s.NATS.URL == "" {
		errs = append(errs, errors.New("nats.url is required"))
	}
	if s.NATS.Creds != "" && (s.NATS.User != "" || s.NATS.Password != "") {
		errs = append(errs, errors.New("nats.creds and nats.user/password are mutually exclusive"))
	}
	if (s.NATS.TLS.Cert == "") != (s.NATS.TLS.Key == "") {
		errs = append(errs, errors.New("nats.tls.cert and nats.tls.key must be set together"))
	}
	if s.Retention.D() <= 0 {
		errs = append(errs, errors.New("retention must be positive"))
	}
	if ar := s.ArchiveRetention.D(); ar < 0 || (ar > 0 && ar < s.Retention.D()) {
		errs = append(errs, errors.New("archive_retention must be 0 (keep every hour) or at least retention, which a rebuild loads"))
	}
	errs = append(errs, validateAccounts(s.Accounts)...)
	return append(errs, s.Kubernetes.validate(events)...)
}

// validateSystemDirs refuses two systems that would write to the same data
// or archive directory.
func validateSystemDirs(systems []System) []error {
	var errs []error
	owner := map[string]string{}
	for i, s := range systems {
		for _, d := range []struct{ kind, dir string }{{"data dir", s.DataDir}, {"archive dir", s.ArchiveDir}} {
			key := cleanDir(d.dir)
			who := fmt.Sprintf("systems[%d] (%s) %s", i, s.ID, d.kind)
			if prev, ok := owner[key]; ok {
				errs = append(errs, fmt.Errorf("%s and %s are the same directory %s", prev, who, d.dir))
				continue
			}
			owner[key] = who
		}
	}
	return errs
}

func cleanDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

func prefixErrors(prefix string, errs []error) []error {
	out := make([]error, len(errs))
	for i, err := range errs {
		out[i] = fmt.Errorf("%s: %w", prefix, err)
	}
	return out
}

// systemsLogValue lists the systems without their passwords.
func systemsLogValue(systems []SystemConfig) slog.Value {
	attrs := make([]slog.Attr, 0, len(systems))
	for _, s := range systems {
		attrs = append(attrs, slog.Group(s.ID,
			slog.String("nats_url", redactURL(s.NATS.URL)),
			slog.String("nats_creds", s.NATS.Creds),
			slog.String("nats_user", s.NATS.User),
			slog.Bool("nats_password_set", s.NATS.Password != ""),
			slog.String("data_dir", s.DataDir),
			slog.String("archive_dir", s.ArchiveDir),
			slog.Any("accounts", accountsLogValue(s.Accounts)),
			slog.String("inventory_file", s.InventoryFile),
			slog.Bool("kubernetes", s.Kubernetes.Enabled),
		))
	}
	return slog.GroupValue(attrs...)
}
