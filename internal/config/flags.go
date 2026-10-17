package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Parse builds the configuration from defaults, the optional YAML file named
// by -config, flags set on the command line and the password environment
// variable, in that order of increasing precedence for each value.
//
// The password has no flag: a flag would show in the process list. The
// password variable is read only when a user is set, since it belongs to
// user/password login.
func Parse(name string, args []string) (Config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	path := fs.String("config", "", "path to a YAML configuration file")

	// Flag values are collected separately and applied after the file,
	// so that only flags actually given override it.
	var f Config
	fs.Var(&f.Interval, "interval", "time between epochs (default 30s)")
	fs.Var(&f.Timeout, "timeout", "collect window for one request (default 3s)")
	fs.StringVar(&f.DataDir, "data-dir", "", "directory for the DuckDB file (default ./data)")
	fs.Var(&f.Retention, "retention", "how long epochs stay in DuckDB, e.g. 14d (default 14d)")
	fs.StringVar(&f.ArchiveDir, "archive-dir", "", "directory for the hourly Parquet archive (default data-dir/archive)")
	fs.Var(&f.ServerForgetAfter, "server-forget-after", "how long a server that stopped answering is still expected (default 1h)")
	fs.Var(&f.ShutdownGrace, "shutdown-grace", "how long an epoch write may continue after a stop signal (default 5s)")
	fs.IntVar(&f.ConnzLimit, "connz-limit", 0, "most connections stored per server per epoch (default 4096)")
	fs.StringVar(&f.Listen, "listen", "", "address for the API and UI in natslens run; empty disables them (default 127.0.0.1:8080)")
	allowed := fs.String("allowed-host", "", "comma-separated Host values the API accepts (default: loopback names when listening on loopback, else any)")
	fs.StringVar(&f.IAPAudience, "iap-audience", "", "IAP audience; when set, mutes need a verified IAP assertion and record its user")
	fs.StringVar(&f.InventoryFile, "inventory-file", "", "YAML file listing the expected streams, consumers and buckets")
	fs.StringVar(&f.LogLevel, "log-level", "", "debug, info, warn or error (default info)")
	fs.StringVar(&f.NATS.URL, "nats-url", "", "system account NATS URL (default nats://127.0.0.1:4222); "+
		"a password written into the URL shows in the process list, use the file or "+PasswordEnv)
	fs.StringVar(&f.NATS.Creds, "nats-creds", "", "system account credentials file")
	fs.StringVar(&f.NATS.User, "nats-user", "", "system account user; the password comes from the file or "+PasswordEnv)
	fs.StringVar(&f.NATS.TLS.CA, "nats-tls-ca", "", "CA file for the NATS server certificate")
	fs.StringVar(&f.NATS.TLS.Cert, "nats-tls-cert", "", "client certificate file")
	fs.StringVar(&f.NATS.TLS.Key, "nats-tls-key", "", "client key file")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	cfg := Default()
	if *path != "" {
		var err error
		if cfg, err = LoadFile(*path, cfg); err != nil {
			return Config{}, err
		}
	}
	set := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if set["allowed-host"] {
		f.AllowedHosts = SplitList(*allowed)
	}
	if err := refusePerSystemFlags(cfg, set); err != nil {
		return Config{}, err
	}
	cfg = applyFlags(cfg, f, set)
	envErrs := cfg.passwordEnvErrors()
	if cfg.NATS.Password == "" && cfg.NATS.User != "" {
		cfg.NATS.Password = os.Getenv(PasswordEnv)
	}
	cfg.Accounts = fillAccountPasswords(cfg.Accounts)
	cfg.Systems = fillSystemPasswords(cfg.Systems)
	if err := errors.Join(append(envErrs, cfg.Validate())...); err != nil {
		return cfg, err
	}
	// The logger is built from the configuration, so these go to the
	// default logger.
	for _, w := range cfg.Warnings() {
		slog.Warn(w)
	}
	return cfg, nil
}

func applyFlags(cfg, f Config, set map[string]bool) Config {
	if set["interval"] {
		cfg.Interval = f.Interval
	}
	if set["timeout"] {
		cfg.Timeout = f.Timeout
	}
	if set["data-dir"] {
		cfg.DataDir = f.DataDir
	}
	if set["retention"] {
		cfg.Retention = f.Retention
	}
	if set["archive-dir"] {
		cfg.ArchiveDir = f.ArchiveDir
	}
	if set["server-forget-after"] {
		cfg.ServerForgetAfter = f.ServerForgetAfter
	}
	if set["shutdown-grace"] {
		cfg.ShutdownGrace = f.ShutdownGrace
	}
	if set["connz-limit"] {
		cfg.ConnzLimit = f.ConnzLimit
	}
	if set["listen"] {
		cfg.Listen = f.Listen
	}
	if set["allowed-host"] {
		cfg.AllowedHosts = f.AllowedHosts
	}
	if set["iap-audience"] {
		cfg.IAPAudience = f.IAPAudience
	}
	if set["log-level"] {
		cfg.LogLevel = f.LogLevel
	}
	if set["inventory-file"] {
		cfg.InventoryFile = f.InventoryFile
	}
	if set["nats-url"] {
		cfg.NATS.URL = f.NATS.URL
	}
	if set["nats-creds"] {
		cfg.NATS.Creds = f.NATS.Creds
	}
	if set["nats-user"] {
		cfg.NATS.User = f.NATS.User
	}
	if set["nats-tls-ca"] {
		cfg.NATS.TLS.CA = f.NATS.TLS.CA
	}
	if set["nats-tls-cert"] {
		cfg.NATS.TLS.Cert = f.NATS.TLS.Cert
	}
	if set["nats-tls-key"] {
		cfg.NATS.TLS.Key = f.NATS.TLS.Key
	}
	return cfg
}

// SplitList splits a comma-separated flag value, dropping empty entries.
func SplitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
