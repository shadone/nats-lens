package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

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
		var errs []error
		if c.SystemID != "" && !systemID.MatchString(c.SystemID) {
			errs = append(errs, fmt.Errorf("system_id %q must match %s", c.SystemID, systemID))
		}
		return append(errs, validatePasswordEnvs(accountPasswordEnvs("", c.Accounts, AccountPasswordEnv))...)
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
	valid := validIDs(c.Systems)
	errs = append(errs, validateSystemDirs(resolved, valid)...)
	return append(errs, validateSystemPasswordEnvs(c.Systems, valid)...)
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

// validIDs reports, per index, whether a listed system has a well-formed id
// that no earlier system uses; the other checks skip the rest, which are
// already reported.
func validIDs(systems []SystemConfig) []bool {
	out := make([]bool, len(systems))
	seen := map[string]bool{}
	for i, s := range systems {
		out[i] = systemID.MatchString(s.ID) && !seen[s.ID]
		seen[s.ID] = true
	}
	return out
}

// validateSystemDirs refuses two systems whose directories overlap: an
// archive equal to or inside another system's archive or data directory, a
// data directory inside another system's archive, or two equal data
// directories. An archive is listed recursively, so a nested one would be
// read and pruned by both systems. A data directory inside another one is
// allowed: each holds only its own files.
func validateSystemDirs(systems []System, valid []bool) []error {
	type dirs struct{ who, data, archive string }
	var ds []dirs
	for i, s := range systems {
		if valid[i] {
			ds = append(ds, dirs{fmt.Sprintf("systems[%d] (%s)", i, s.ID), realDir(s.DataDir), realDir(s.ArchiveDir)})
		}
	}
	var errs []error
	for i := range ds {
		for j := i + 1; j < len(ds); j++ {
			if msg := dirClash(ds[i].data, ds[i].archive, ds[j].data, ds[j].archive); msg != "" {
				errs = append(errs, fmt.Errorf("%s and %s: %s", ds[i].who, ds[j].who, msg))
			}
		}
	}
	return errs
}

// dirClash describes how the directories of two systems a and b overlap,
// or returns "" when they do not.
func dirClash(aData, aArchive, bData, bArchive string) string {
	switch {
	case aData == bData:
		return fmt.Sprintf("both use the same directory %s as data directory", aData)
	case aArchive == bArchive:
		return fmt.Sprintf("both use the same directory %s as archive directory", aArchive)
	case within(aArchive, bArchive) || within(bArchive, aArchive):
		return fmt.Sprintf("one archive directory is inside the other: %s and %s", aArchive, bArchive)
	case within(aArchive, bData):
		return fmt.Sprintf("the first's archive directory %s is inside the second's data directory %s", aArchive, bData)
	case within(bArchive, aData):
		return fmt.Sprintf("the second's archive directory %s is inside the first's data directory %s", bArchive, aData)
	case within(aData, bArchive):
		return fmt.Sprintf("the first's data directory %s is inside the second's archive directory %s", aData, bArchive)
	case within(bData, aArchive):
		return fmt.Sprintf("the second's data directory %s is inside the first's archive directory %s", bData, aArchive)
	}
	return ""
}

// within reports whether dir is parent or inside it.
func within(dir, parent string) bool {
	if dir == parent {
		return true
	}
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}
	return strings.HasPrefix(dir, parent)
}

// realDir returns dir as an absolute path with the symbolic links of its
// longest existing prefix resolved, so that two names of one directory
// compare equal before either exists in full.
func realDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	p, rest := abs, ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return abs
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// passwordEnv is an environment variable a connection reads its password
// from, and the connection that reads it.
type passwordEnv struct{ name, who string }

// accountPasswordEnvs lists the variables the accounts with a user read;
// an account whose name repeats an earlier one is left out, as that is
// already reported.
func accountPasswordEnvs(prefix string, accs []Account, env func(string) string) []passwordEnv {
	var out []passwordEnv
	seen := map[string]bool{}
	for _, a := range accs {
		if a.User == "" || seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		out = append(out, passwordEnv{env(a.Name), prefix + "account " + a.Name})
	}
	return out
}

// validateSystemPasswordEnvs refuses two connections of the listed systems
// that would read their password from the same variable.
func validateSystemPasswordEnvs(systems []SystemConfig, valid []bool) []error {
	var envs []passwordEnv
	for i, s := range systems {
		if !valid[i] {
			continue
		}
		who := fmt.Sprintf("systems[%d] (%s) ", i, s.ID)
		if s.NATS.User != "" {
			envs = append(envs, passwordEnv{SystemPasswordEnv(s.ID), who + "nats"})
		}
		envs = append(envs, accountPasswordEnvs(who, s.Accounts, func(name string) string {
			return SystemAccountPasswordEnv(s.ID, name)
		})...)
	}
	return validatePasswordEnvs(envs)
}

func validatePasswordEnvs(envs []passwordEnv) []error {
	var errs []error
	first := map[string]string{}
	for _, e := range envs {
		if prev, ok := first[e.name]; ok {
			errs = append(errs, fmt.Errorf("%s and %s both read their password from %s: rename one", prev, e.who, e.name))
			continue
		}
		first[e.name] = e.who
	}
	return errs
}

func prefixErrors(prefix string, errs []error) []error {
	out := make([]error, len(errs))
	for i, err := range errs {
		out[i] = fmt.Errorf("%s: %w", prefix, err)
	}
	return out
}
