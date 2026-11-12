package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// DefaultSystemID is the id of the one system of a configuration without
// systems when system_id is not set.
const DefaultSystemID = "default"

// DefaultNATSName is the connection name the servers show for nats-lens
// when nats.name is not set; a listed system's default is
// DefaultNATSName-<id>.
const DefaultNATSName = "natslens"

// MinSystemMemoryLimit is the least DuckDB memory a listed system without
// its own limit gets, however little of the budget is left.
const MinSystemMemoryLimit = 256 << 20

// systemID is the form of a system id: it appears in URLs, the API, metric
// labels and directory names.
var systemID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// reservedSystemIDs are the system label values /metrics gives timings
// that belong to no one system: all, a call without a system; unknown, a
// call naming no configured one; other, what the series bounds leave out.
var reservedSystemIDs = map[string]bool{"all": true, "unknown": true, "other": true}

// checkSystemID returns why id cannot name a system, or nil.
func checkSystemID(id string) error {
	if !systemID.MatchString(id) {
		return fmt.Errorf("%q must match %s", id, systemID)
	}
	if reservedSystemIDs[id] {
		return fmt.Errorf("%q is reserved: /metrics labels timings that belong to no one system all, unknown and other", id)
	}
	return nil
}

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
	// NATS is the system account connection; nats.name defaults to
	// natslens-<id>.
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
	// DuckDBMemory bounds the memory of the system's DuckDB; zero means an
	// equal share of what the systems' own limits leave of the top-level
	// budget, at least MinSystemMemoryLimit. The systems' total exceeds
	// the budget when their own limits do, or when the floor applies;
	// natslens warns then.
	DuckDBMemory ByteSize `yaml:"duckdb_memory_limit"`
	// DuckDBMaxTemp bounds the disk the system's DuckDB may spill to;
	// zero means the top-level duckdb_max_temp_size, else a bound derived
	// when the system's store opens (DuckDBMaxTempSizeFor).
	DuckDBMaxTemp ByteSize `yaml:"duckdb_max_temp_size"`
}

// UnmarshalYAML starts each system from its defaults, so that a key left
// out keeps its default rather than its zero value. It uses the decoder's
// own unmarshal function, so unknown keys are still refused.
func (s *SystemConfig) UnmarshalYAML(unmarshal func(any) error) error {
	type plain SystemConfig
	p := plain{Kubernetes: defaultKubernetes()}
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
	// DuckDBMaxTempSize is the system's own or the top-level
	// duckdb_max_temp_size; zero means derived when the store opens.
	DuckDBMaxTempSize int64
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
	share := c.memoryShare()
	out := make([]System, 0, len(c.Systems))
	for _, s := range c.Systems {
		out = append(out, c.resolve(s, share))
	}
	return out
}

// explicitMemory sums the listed systems' own DuckDB limits and counts
// the systems without one.
func (c Config) explicitMemory() (sum int64, unset int64) {
	for _, s := range c.Systems {
		if s.DuckDBMemory > 0 {
			sum += int64(s.DuckDBMemory)
		} else {
			unset++
		}
	}
	return sum, unset
}

// memoryShare is the DuckDB limit of a listed system without its own: an
// equal share of what the own limits leave of the budget, at least
// MinSystemMemoryLimit.
func (c Config) memoryShare() int64 {
	sum, unset := c.explicitMemory()
	if unset == 0 {
		return 0
	}
	return max(MinSystemMemoryLimit, max(0, c.DuckDBMemoryLimit()-sum)/unset)
}

// memoryWarning says when the listed systems' resolved DuckDB limits add
// up to more than the budget: because their own limits do, or because of
// the MinSystemMemoryLimit floor of the systems without one.
func (c Config) memoryWarning() string {
	budget := c.DuckDBMemoryLimit()
	var total int64
	for _, s := range c.ResolvedSystems() {
		total += s.DuckDBMemoryLimit
	}
	if total <= budget {
		return ""
	}
	if own, _ := c.explicitMemory(); own > budget {
		return fmt.Sprintf("the systems' own duckdb_memory_limit values add up to %d bytes, more than the budget of %d bytes", own, budget)
	}
	return fmt.Sprintf("the systems' duckdb_memory_limit values add up to %d bytes, more than the budget of %d bytes, "+
		"because each system without its own limit gets at least the floor of %d bytes", total, budget, MinSystemMemoryLimit)
}

// Warnings lists what is accepted but probably not meant: listed systems'
// DuckDB limits above the budget, and password variables of a
// single-system file set while systems are used, which are then ignored.
// Values of variables are never included.
func (c Config) Warnings() []string {
	if len(c.Systems) == 0 {
		return nil
	}
	var out []string
	if w := c.memoryWarning(); w != "" {
		out = append(out, w)
	}
	var legacy []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == PasswordEnv || (strings.HasPrefix(name, "NATSLENS_ACCOUNT_") && strings.HasSuffix(name, "_PASSWORD")) {
			legacy = append(legacy, name)
		}
	}
	if len(legacy) > 0 {
		sort.Strings(legacy)
		out = append(out, fmt.Sprintf("%s ignored: with systems, passwords come from NATSLENS_SYSTEM_<ID>_NATS_PASSWORD and NATSLENS_SYSTEM_<ID>_ACCOUNT_<NAME>_PASSWORD", strings.Join(legacy, ", ")))
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
		DuckDBMaxTempSize: int64(c.DuckDBMaxTemp),
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
	if out.NATS.Name == "" {
		out.NATS.Name = DefaultNATSName + "-" + s.ID
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
	out.DuckDBMaxTempSize = int64(c.DuckDBMaxTemp)
	if s.DuckDBMaxTemp > 0 {
		out.DuckDBMaxTempSize = int64(s.DuckDBMaxTemp)
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

// systemIDs lists the ids of the configured systems.
func (c Config) systemIDs() []string {
	systems := c.ResolvedSystems()
	out := make([]string, len(systems))
	for i, s := range systems {
		out[i] = s.ID
	}
	return out
}
