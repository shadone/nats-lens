package config

import (
	"errors"
	"fmt"
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
		if c.SystemID != "" {
			if err := checkSystemID(c.SystemID); err != nil {
				errs = append(errs, fmt.Errorf("system_id %w", err))
			}
		}
		return errs
	}
	var errs []error
	if c.SystemID != "" {
		errs = append(errs, errors.New("system_id cannot be used with systems: give each system its id"))
	}
	seen := map[string]bool{}
	for i, s := range c.Systems {
		p := fmt.Sprintf("systems[%d]", i)
		switch err := checkSystemID(s.ID); {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: id %w", p, err))
		case seen[s.ID]:
			errs = append(errs, fmt.Errorf("%s: duplicate id %q", p, s.ID))
		}
		seen[s.ID] = true
	}
	resolved := c.ResolvedSystems()
	for i, s := range resolved {
		errs = append(errs, prefixErrors(fmt.Sprintf("systems[%d]", i), s.validate(c.Events))...)
	}
	return append(errs, validateSystemDirs(resolved, validIDs(c.Systems))...)
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
		out[i] = checkSystemID(s.ID) == nil && !seen[s.ID]
		seen[s.ID] = true
	}
	return out
}

// passwordEnv is an environment variable a connection reads its password
// from, and the connection that reads it.
type passwordEnv struct{ name, who string }

// readsPasswordEnv reports whether a connection takes its password from
// the environment: it has a user and the file gives no password.
func readsPasswordEnv(user, password string) bool { return user != "" && password == "" }

// passwordEnvErrors refuses two connections that would read their password
// from the same environment variable. It runs before the variables are
// read, as only then does an empty password show that one is read.
func (c Config) passwordEnvErrors() []error {
	if len(c.Systems) == 0 {
		return validatePasswordEnvs(accountPasswordEnvs("", c.Accounts, AccountPasswordEnv))
	}
	return validateSystemPasswordEnvs(c.Systems, validIDs(c.Systems))
}

// accountPasswordEnvs lists the variables the accounts read their
// password from; an account whose name repeats an earlier one is left
// out, as that is already reported.
func accountPasswordEnvs(prefix string, accs []Account, env func(string) string) []passwordEnv {
	var out []passwordEnv
	seen := map[string]bool{}
	for _, a := range accs {
		if !readsPasswordEnv(a.User, a.Password) || seen[a.Name] {
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
		if readsPasswordEnv(s.NATS.User, s.NATS.Password) {
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
			errs = append(errs, fmt.Errorf("%s and %s both read their password from %s: give the accounts (or systems) names that differ in letters or digits, or set password in the file", prev, e.who, e.name))
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
