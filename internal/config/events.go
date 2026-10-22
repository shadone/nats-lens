package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Events configures event capture: the system account subscriptions and
// the JetStream advisories of each account in Accounts.
type Events struct {
	// Enabled turns event capture on. The snapshot collector runs either way.
	Enabled bool `yaml:"enabled"`
	// APIAudit stores every JetStream API audit advisory as an event. Audits
	// are always read to name the client behind a create, update or delete;
	// storing all of them is off by default because every API call makes one.
	APIAudit bool `yaml:"api_audit"`
	// AccountConnections stores the connection count every server sends
	// for every account every 30 seconds; off by default.
	AccountConnections bool `yaml:"account_connections"`
	// FlushInterval is the longest an event waits before it is written.
	FlushInterval Duration `yaml:"flush_interval"`
	// BatchSize is the number of buffered events that triggers a write.
	BatchSize int `yaml:"batch_size"`
	// Churn collapses short-lived connections that close themselves.
	Churn Churn `yaml:"churn"`
}

// Churn configures the collapsing of connect and disconnect pairs: a
// connect whose disconnect, with reason "Client Closed", follows within
// PairWithin is not stored; one client.churn event per client name and
// Window counts them instead.
type Churn struct {
	Collapse   bool     `yaml:"collapse"`
	PairWithin Duration `yaml:"pair_within"`
	Window     Duration `yaml:"window"`
}

// Account is a JetStream account whose advisories are captured, with its own
// connection. Advisories are published inside the account, so the system
// account cannot see them.
type Account struct {
	// Name labels the account in events and logs; use the account name the
	// servers know it by (for example APP).
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	Creds    string `yaml:"creds"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	TLS      TLS    `yaml:"tls"`
	// Micro, when false, stops asking the account's micro services
	// ($SRV.INFO and $SRV.STATS); unset asks them.
	Micro *bool `yaml:"micro"`
}

// AsksMicro reports whether the account's micro services are asked.
func (a Account) AsksMicro() bool { return a.Micro == nil || *a.Micro }

// AccountPasswordEnv returns the environment variable read for an account's
// password when the file does not set one: NATSLENS_ACCOUNT_<NAME>_PASSWORD,
// with the name upper-cased and every character other than A-Z and 0-9
// replaced by an underscore.
func AccountPasswordEnv(name string) string {
	return "NATSLENS_ACCOUNT_" + envName(name) + "_PASSWORD"
}

// envName upper-cases a name for an environment variable and replaces
// every character other than A-Z and 0-9 by an underscore.
func envName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Connection returns the account's connection settings. An empty URL falls
// back to the system connection's URL and an empty CA to its CA; the client
// certificate is never inherited, as it is the system user's identity.
func (a Account) Connection(sys NATS) NATS {
	n := NATS{
		URL:      a.URL,
		Name:     sys.Name + "-" + a.Name,
		Creds:    a.Creds,
		User:     a.User,
		Password: a.Password,
		TLS:      a.TLS,
	}
	if n.URL == "" {
		n.URL = sys.URL
	}
	if n.TLS.CA == "" {
		n.TLS.CA = sys.TLS.CA
	}
	return n
}

// ResolvedArchiveDir returns the archive directory: archive_dir when set,
// otherwise data_dir/archive.
func (c Config) ResolvedArchiveDir() string {
	if c.ArchiveDir != "" {
		return c.ArchiveDir
	}
	return filepath.Join(c.DataDir, "archive")
}

// fillAccountPasswords reads each account's password from its environment
// variable when the file sets a user but no password.
func fillAccountPasswords(accs []Account) []Account {
	out := make([]Account, len(accs))
	for i, a := range accs {
		if a.Password == "" && a.User != "" {
			a.Password = os.Getenv(AccountPasswordEnv(a.Name))
		}
		out[i] = a
	}
	return out
}

func (e Events) validate() []error {
	var errs []error
	// The archive keeps an hour open for two flush intervals after it ends,
	// so a long flush interval delays every hour's export.
	if e.FlushInterval.D() <= 0 || e.FlushInterval.D() > MaxFlushInterval {
		errs = append(errs, fmt.Errorf("events.flush_interval must be positive and at most %s", MaxFlushInterval))
	}
	if e.BatchSize <= 0 {
		errs = append(errs, errors.New("events.batch_size must be positive"))
	}
	if c := e.Churn; c.PairWithin.D() <= 0 || c.Window.D() < c.PairWithin.D() {
		errs = append(errs, errors.New("events.churn.pair_within must be positive and at most events.churn.window"))
	}
	return errs
}

func validateAccounts(accs []Account) []error {
	var errs []error
	seen := map[string]bool{}
	for i, a := range accs {
		switch {
		case a.Name == "":
			errs = append(errs, fmt.Errorf("accounts[%d]: name is required", i))
		case seen[a.Name]:
			errs = append(errs, fmt.Errorf("accounts[%d]: duplicate name %q", i, a.Name))
		}
		seen[a.Name] = true
		if a.Creds != "" && (a.User != "" || a.Password != "") {
			errs = append(errs, fmt.Errorf("accounts[%d]: creds and user/password are mutually exclusive", i))
		}
		if (a.TLS.Cert == "") != (a.TLS.Key == "") {
			errs = append(errs, fmt.Errorf("accounts[%d]: tls.cert and tls.key must be set together", i))
		}
	}
	return errs
}

// accountsLogValue lists the accounts without their passwords.
func accountsLogValue(accs []Account) slog.Value {
	attrs := make([]slog.Attr, 0, len(accs))
	for _, a := range accs {
		attrs = append(attrs, slog.Attr{Key: a.Name, Value: a.LogValue()})
	}
	return slog.GroupValue(attrs...)
}

// MaxFlushInterval is the longest events.flush_interval allowed.
const MaxFlushInterval = 5 * time.Minute

// defaultEvents is the event capture configuration used when nothing
// overrides it.
func defaultEvents() Events {
	return Events{Enabled: true, FlushInterval: Duration(time.Second), BatchSize: 500,
		Churn: Churn{Collapse: true, PairWithin: Duration(10 * time.Second), Window: Duration(time.Minute)}}
}
