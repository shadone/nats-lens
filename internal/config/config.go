// Package config loads the natslens configuration from a YAML file and
// command-line flags. Flags given on the command line override the file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Config is the full natslens configuration.
type Config struct {
	// Interval is the time between the starts of two epochs.
	Interval Duration `yaml:"interval"`
	// Timeout is the collect window: how long one request waits for
	// replies from all servers.
	Timeout Duration `yaml:"timeout"`
	// DataDir holds the DuckDB file.
	DataDir string `yaml:"data_dir"`
	// Retention is how long epochs stay in DuckDB.
	Retention Duration `yaml:"retention"`
	// ServerForgetAfter is how long a server that stopped answering is still
	// counted as expected.
	ServerForgetAfter Duration `yaml:"server_forget_after"`
	// ShutdownGrace is how long an epoch write that is running when the
	// process is told to stop may continue.
	ShutdownGrace Duration `yaml:"shutdown_grace"`
	// ConnzLimit is the most connections requested from one server per epoch.
	ConnzLimit int `yaml:"connz_limit"`
	// SubscriptionsInterval is the shortest time between two collections
	// of the servers' subscription lists (SUBSZ); 0 turns them off.
	SubscriptionsInterval Duration `yaml:"subscriptions_interval"`
	// SubszLimit is the most subscriptions requested from one server per
	// subscription collection.
	SubszLimit int `yaml:"subsz_limit"`
	// LogLevel is debug, info, warn or error.
	LogLevel string `yaml:"log_level"`
	// NATS is the system account connection.
	NATS NATS `yaml:"nats"`
	// ArchiveDir receives the hourly Parquet files; empty means data_dir/archive.
	ArchiveDir string `yaml:"archive_dir"`
	// ArchiveProvisionalEvery is how often the open hour is exported as it
	// is so far; 0 disables provisional exports.
	ArchiveProvisionalEvery Duration `yaml:"archive_provisional_every"`
	// ArchiveRetention is how long archived hours are kept; older hours
	// are removed after an export. 0 keeps them all.
	ArchiveRetention Duration `yaml:"archive_retention"`
	// Listen is the address the API and UI are served on in natslens run;
	// empty disables them.
	Listen string `yaml:"listen"`
	// AllowedHosts are the Host header values the API accepts; empty means
	// the loopback names when Listen is a loopback address, and any host
	// otherwise.
	AllowedHosts []string `yaml:"allowed_hosts"`
	// IAPAudience, when set, makes the API verify the signed IAP header and
	// record its user as the one who mutes; the value is the backend
	// service's audience, /projects/<number>/global/backendServices/<id>.
	IAPAudience string `yaml:"iap_audience"`
	// MetricsTimeout bounds each system's part of a /metrics scrape; keep
	// it below the Prometheus scrape_timeout.
	MetricsTimeout Duration `yaml:"metrics_timeout"`
	// AllowUnauthenticated lets the API listen beyond loopback without
	// iap_audience, so that anyone who reaches it may mute findings.
	AllowUnauthenticated bool `yaml:"allow_unauthenticated"`
	// DuckDBMemory bounds the memory DuckDB uses; zero means 60% of the
	// container's memory limit, or 1 GiB without one.
	DuckDBMemory ByteSize `yaml:"duckdb_memory_limit"`
	// DuckDBMaxTemp bounds the disk each system's DuckDB may spill to;
	// zero derives it from the system's memory limit and query budget
	// (DuckDBMaxTempSizeFor).
	DuckDBMaxTemp ByteSize `yaml:"duckdb_max_temp_size"`
	// QueryBudget is how many queries of the API and /metrics may run at
	// once on each system's database; zero derives it from the database's
	// threads (QueryBudgetFor), -1 turns the budget off.
	QueryBudget int `yaml:"query_budget"`
	// Events configures event capture.
	Events Events `yaml:"events"`
	// Accounts are the JetStream accounts whose advisories are captured.
	Accounts []Account `yaml:"accounts"`
	// InventoryFile lists the expected streams, consumers and buckets;
	// empty means no inventory checks fire.
	InventoryFile string `yaml:"inventory_file"`
	// Checks tunes the checks run after every epoch.
	Checks Checks `yaml:"checks"`
	// Notifications configures the channels findings are sent to.
	Notifications Notifications `yaml:"notifications"`
	// Kubernetes configures the optional Kubernetes watcher.
	Kubernetes Kubernetes `yaml:"kubernetes"`
	// SystemID names the one system of a configuration without Systems;
	// empty means DefaultSystemID.
	SystemID string `yaml:"system_id"`
	// Systems lists the monitored NATS systems. When it is set, DataDir and
	// ArchiveDir are the roots of the systems' directories, DuckDBMemory is
	// the budget they share, Retention and ArchiveRetention are their
	// defaults, and the top-level nats, accounts, inventory_file and
	// kubernetes keys are refused.
	Systems []SystemConfig `yaml:"systems"`
}

// Checks tunes the checks. Codes and threshold names are checked against
// the check files when natslens starts, not here.
type Checks struct {
	// Disabled lists check codes that do not run; their findings resolve.
	Disabled []string `yaml:"disabled"`
	// Thresholds overrides threshold defaults: code to threshold name to
	// value, in the threshold's unit ("0.9", "5m", "2GiB").
	Thresholds map[string]map[string]string `yaml:"thresholds"`
	// Timeout bounds one check's query.
	Timeout Duration `yaml:"timeout"`
	// RestartWindow is how long after a restart a server counts as
	// restarting, so that checks leave its own reports out.
	RestartWindow Duration `yaml:"restart_window"`
	// IncludeTooling makes the client checks count tooling connections:
	// nats-lens's own and clients connected from a loopback address.
	IncludeTooling bool `yaml:"include_tooling"`
	// LoopbackIsTooling makes clients connected from 127.0.0.1 or ::1 (a
	// port-forwarded CLI) tooling; nil means true.
	LoopbackIsTooling *bool `yaml:"loopback_is_tooling"`
	// RemediationFile is a YAML file that maps check codes to text appended
	// to their remediation; empty means none.
	RemediationFile string `yaml:"remediation_file"`
}

// LoopbackTooling is LoopbackIsTooling with its default.
func (c Checks) LoopbackTooling() bool { return c.LoopbackIsTooling == nil || *c.LoopbackIsTooling }

// ToolingNames are the connection names nats-lens's own connections use:
// the collector's, its event connection's and each account connection's.
func (c Config) ToolingNames() []string {
	out := []string{c.NATS.Name, c.NATS.Name + "-events"}
	for _, a := range c.Accounts {
		out = append(out, a.Connection(c.NATS).Name)
	}
	return out
}

// NATS describes the connection to the monitored system's system account.
type NATS struct {
	URL      string `yaml:"url"`
	Name     string `yaml:"name"`
	Creds    string `yaml:"creds"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	TLS      TLS    `yaml:"tls"`
}

// TLS holds client TLS files. All are optional.
type TLS struct {
	CA   string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// MaxSubszLimit is the largest subsz_limit. Every server's SUBSZ reply
// arrives on the collector's one connection within one window, with the
// CONNZ replies, and a server closes a connection whose unsent replies
// pass its max_pending (64 MB by default) as a slow consumer; 100,000
// subscriptions make a reply of about 15 to 25 MB.
const MaxSubszLimit = 100000

// PasswordEnv is the environment variable read for the NATS password when
// neither the file nor a flag sets it.
const PasswordEnv = "NATSLENS_NATS_PASSWORD"

// Default returns the configuration used when nothing overrides it.
func Default() Config {
	return Config{
		Interval:              Duration(30 * time.Second),
		Timeout:               Duration(3 * time.Second),
		DataDir:               "./data",
		Retention:             Duration(14 * 24 * time.Hour),
		ServerForgetAfter:     Duration(time.Hour),
		ShutdownGrace:         Duration(5 * time.Second),
		ConnzLimit:            4096,
		SubscriptionsInterval: Duration(5 * time.Minute),
		SubszLimit:            10000,
		LogLevel:              "info",
		NATS: NATS{
			URL:  "nats://127.0.0.1:4222",
			Name: DefaultNATSName,
		},
		Events:                  defaultEvents(),
		ArchiveProvisionalEvery: Duration(5 * time.Minute),
		ArchiveRetention:        Duration(90 * 24 * time.Hour),
		Listen:                  "127.0.0.1:8080",
		MetricsTimeout:          Duration(8 * time.Second),
		Checks:                  Checks{Timeout: Duration(5 * time.Second), RestartWindow: Duration(10 * time.Minute)},
		Kubernetes:              defaultKubernetes(),
	}
}

// LoadFile reads a YAML file over the given base configuration.
// Unknown keys are an error so that a typo does not pass silently.
func LoadFile(path string, base Config) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return base, fmt.Errorf("open config: %w", err)
	}
	return decodeFile(path, data, base)
}

// decodeFile reads the YAML data of the file at path over base; on an
// error it returns base.
func decodeFile(path string, data []byte, base Config) (Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// The decoder writes into the maps and pointers it finds, so give it
	// copies of the base's.
	cfg := base
	if base.Checks.Thresholds != nil {
		cfg.Checks.Thresholds = make(map[string]map[string]string, len(base.Checks.Thresholds))
		for code, values := range base.Checks.Thresholds {
			cfg.Checks.Thresholds[code] = maps.Clone(values)
		}
	}
	if p := base.Checks.LoopbackIsTooling; p != nil {
		loopback := *p
		cfg.Checks.LoopbackIsTooling = &loopback
	}
	if err := dec.Decode(&cfg); err != nil {
		return base, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Defaults hide whether a key was written, so the keys a list of
	// systems refuses are read from the file itself.
	var keys map[string]any
	if err := yaml.Unmarshal(data, &keys); err != nil {
		return base, fmt.Errorf("parse config %s: %w", path, err)
	}
	present := make(map[string]bool, len(keys))
	for k := range keys {
		present[k] = true
	}
	if present["systems"] && len(cfg.Systems) == 0 {
		return base, fmt.Errorf("config %s: systems is empty: list at least one system, or remove the key", path)
	}
	if err := refusePerSystemKeys(present); err != nil {
		return base, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks values that would make the collector misbehave.
func (c Config) Validate() error {
	var errs []error
	if c.Interval.D() <= 0 {
		errs = append(errs, errors.New("interval must be positive"))
	}
	if c.Timeout.D() <= 0 {
		errs = append(errs, errors.New("timeout must be positive"))
	}
	// An epoch is discovery, the detail pass and, when JSZ shows a new
	// JetStream account, one more RAFTZ pass: up to three collect windows.
	if 3*c.Timeout.D() >= c.Interval.D() {
		errs = append(errs, errors.New("timeout must be less than a third of interval: an epoch can take three collect windows"))
	}
	// An epoch write never runs longer than interval, so a longer grace
	// would never be used.
	if c.ShutdownGrace.D() <= 0 || c.ShutdownGrace.D() > c.Interval.D() {
		errs = append(errs, errors.New("shutdown_grace must be positive and at most interval"))
	}
	if c.MetricsTimeout.D() <= 0 {
		errs = append(errs, errors.New("metrics_timeout must be positive"))
	}
	if c.QueryBudget < -1 {
		errs = append(errs, errors.New("query_budget must be -1 (off), 0 (derived from the threads) or a number of queries"))
	}
	if c.ServerForgetAfter.D() <= 0 {
		errs = append(errs, errors.New("server_forget_after must be positive"))
	}
	if c.Retention.D() <= 0 {
		errs = append(errs, errors.New("retention must be positive"))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir is required"))
	}
	if c.ConnzLimit <= 0 {
		errs = append(errs, errors.New("connz_limit must be positive"))
	}
	if c.SubscriptionsInterval.D() < 0 {
		errs = append(errs, errors.New("subscriptions_interval must not be negative; 0 turns subscription collections off"))
	}
	if c.SubszLimit <= 0 {
		errs = append(errs, errors.New("subsz_limit must be positive"))
	} else if c.SubszLimit > MaxSubszLimit {
		errs = append(errs, fmt.Errorf("subsz_limit must be at most %d: every server's list arrives on one connection, "+
			"which a server closes as a slow consumer past its max_pending", MaxSubszLimit))
	}
	if c.NATS.URL == "" {
		errs = append(errs, errors.New("nats.url is required"))
	}
	if c.NATS.Creds != "" && (c.NATS.User != "" || c.NATS.Password != "") {
		errs = append(errs, errors.New("nats.creds and nats.user/password are mutually exclusive"))
	}
	if (c.NATS.TLS.Cert == "") != (c.NATS.TLS.Key == "") {
		errs = append(errs, errors.New("nats.tls.cert and nats.tls.key must be set together"))
	}
	if c.Listen != "" {
		if _, _, err := net.SplitHostPort(c.Listen); err != nil {
			errs = append(errs, fmt.Errorf("listen: %w", err))
		}
	}
	if _, err := ParseLevel(c.LogLevel); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, c.Events.validate()...)
	errs = append(errs, c.Kubernetes.validate(c.Events)...)
	errs = append(errs, c.validateAuth()...)
	if c.ArchiveProvisionalEvery.D() < 0 {
		errs = append(errs, errors.New("archive_provisional_every must not be negative"))
	}
	if ar := c.ArchiveRetention.D(); ar < 0 || (ar > 0 && ar < c.Retention.D()) {
		errs = append(errs, errors.New("archive_retention must be 0 (keep every hour) or at least retention, which a rebuild loads"))
	}
	errs = append(errs, validateAccounts(c.Accounts)...)
	errs = append(errs, c.validateSystems()...)
	errs = append(errs, c.Notifications.validate(c.systemIDs())...)
	if c.Checks.Timeout.D() <= 0 || c.Checks.Timeout.D() > c.Interval.D() {
		errs = append(errs, errors.New("checks.timeout must be positive and at most interval"))
	}
	if c.Checks.RestartWindow.D() < 0 {
		errs = append(errs, errors.New("checks.restart_window must not be negative"))
	}
	return errors.Join(errs...)
}

// ParseLevel maps a log level name to slog.Level.
func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("invalid log_level %q", s)
	}
	return l, nil
}

// LogValue keeps credentials out of logs: the password is never written,
// only whether one is set.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("interval", c.Interval.String()),
		slog.String("timeout", c.Timeout.String()),
		slog.String("data_dir", c.DataDir),
		slog.String("retention", c.Retention.String()),
		slog.String("archive_retention", c.ArchiveRetention.String()),
		slog.Int("connz_limit", c.ConnzLimit),
		slog.String("subscriptions_interval", c.SubscriptionsInterval.String()),
		slog.Int("subsz_limit", c.SubszLimit),
		slog.String("log_level", c.LogLevel),
		slog.String("nats_url", redactURL(c.NATS.URL)),
		slog.String("nats_creds", c.NATS.Creds),
		slog.String("nats_user", c.NATS.User),
		slog.Bool("nats_password_set", c.NATS.Password != ""),
		slog.String("nats_tls_ca", c.NATS.TLS.CA),
		slog.String("archive_dir", c.ResolvedArchiveDir()),
		slog.String("listen", c.Listen),
		slog.String("metrics_timeout", c.MetricsTimeout.String()),
		slog.Int("query_budget", c.QueryBudget),
		slog.Any("allowed_hosts", c.AllowedHosts),
		slog.String("iap_audience", c.IAPAudience),
		slog.Bool("allow_unauthenticated", c.AllowUnauthenticated),
		slog.Bool("events", c.Events.Enabled),
		slog.Bool("events_api_audit", c.Events.APIAudit),
		slog.Any("accounts", accountsLogValue(c.Accounts)),
		slog.String("inventory_file", c.InventoryFile),
		slog.Any("checks_disabled", c.Checks.Disabled),
		slog.Any("checks_thresholds", c.Checks.Thresholds),
		slog.String("checks_remediation_file", c.Checks.RemediationFile),
		slog.Any("notifications", c.Notifications.LogValue()),
		slog.Bool("kubernetes", c.Kubernetes.Enabled),
		slog.Any("kubernetes_namespaces", c.Kubernetes.Namespaces),
		slog.String("kubernetes_kubeconfig", c.Kubernetes.Kubeconfig),
		slog.String("system_id", c.SystemID),
		slog.Any("systems", systemsLogValue(c.Systems)),
	)
}

// redactURL hides a password embedded in a URL list ("nats://u:p@host").
// A part that does not parse as a URL with user information but holds an
// "@" has everything between "://" (or its start) and its last "@"
// replaced, user included.
func redactURL(s string) string {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		if u, err := url.Parse(strings.TrimSpace(p)); err == nil && u.User != nil {
			parts[i] = u.Redacted()
		} else if at := strings.LastIndex(p, "@"); at >= 0 {
			start := 0
			if j := strings.Index(p[:at], "://"); j >= 0 {
				start = j + len("://")
			}
			parts[i] = p[:start] + "xxxxx" + p[at:]
		}
	}
	return strings.Join(parts, ",")
}
