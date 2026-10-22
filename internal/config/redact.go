package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// redactedPassword replaces a password in printed values.
const redactedPassword = "[redacted]"

// natsFields and accountFields have the fields of NATS and Account without
// their methods, so that formatting them does not call String again.
type (
	natsFields    NATS
	accountFields Account
)

func (n NATS) redacted() natsFields {
	n.URL = redactURL(n.URL)
	if n.Password != "" {
		n.Password = redactedPassword
	}
	return natsFields(n)
}

func (a Account) redacted() accountFields {
	a.URL = redactURL(a.URL)
	if a.Password != "" {
		a.Password = redactedPassword
	}
	return accountFields(a)
}

// String prints the connection with its password and any password in the
// URL hidden, for %v, %+v and %s.
func (n NATS) String() string { return fmt.Sprintf("%+v", n.redacted()) }

// GoString is String for %#v.
func (n NATS) GoString() string {
	return strings.Replace(fmt.Sprintf("%#v", n.redacted()), "config.natsFields", "config.NATS", 1)
}

// MarshalJSON writes the connection with its password and any password in
// the URL hidden.
func (n NATS) MarshalJSON() ([]byte, error) { return json.Marshal(n.redacted()) }

// LogValue logs the connection without its password.
func (n NATS) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("url", redactURL(n.URL)),
		slog.String("name", n.Name),
		slog.String("creds", n.Creds),
		slog.String("user", n.User),
		slog.Bool("password_set", n.Password != ""),
		slog.String("tls_ca", n.TLS.CA),
		slog.String("tls_cert", n.TLS.Cert),
	)
}

// String prints the account with its password and any password in the
// URL hidden, for %v, %+v and %s.
func (a Account) String() string { return fmt.Sprintf("%+v", a.redacted()) }

// GoString is String for %#v.
func (a Account) GoString() string {
	return strings.Replace(fmt.Sprintf("%#v", a.redacted()), "config.accountFields", "config.Account", 1)
}

// MarshalJSON writes the account with its password and any password in
// the URL hidden.
func (a Account) MarshalJSON() ([]byte, error) { return json.Marshal(a.redacted()) }

// LogValue logs the account without its password.
func (a Account) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("url", redactURL(a.URL)),
		slog.String("creds", a.Creds),
		slog.String("user", a.User),
		slog.Bool("password_set", a.Password != ""),
		slog.String("tls_ca", a.TLS.CA),
		slog.Bool("micro", a.AsksMicro()),
	)
}

// LogValue logs the system without its passwords.
func (s System) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("id", s.ID),
		slog.String("name", s.Name),
		slog.String("environment", s.Environment),
		slog.Any("nats", s.NATS.LogValue()),
		slog.Any("accounts", accountsLogValue(s.Accounts)),
		slog.String("inventory_file", s.InventoryFile),
		slog.Bool("kubernetes", s.Kubernetes.Enabled),
		slog.String("retention", s.Retention.String()),
		slog.String("archive_retention", s.ArchiveRetention.String()),
		slog.String("data_dir", s.DataDir),
		slog.String("archive_dir", s.ArchiveDir),
		slog.Int64("duckdb_memory_limit", s.DuckDBMemoryLimit),
	)
}
