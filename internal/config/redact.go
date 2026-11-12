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

// spillLogAttr logs a system's spill bound, "derived" when it is set when
// the store opens rather than configured.
func spillLogAttr(n int64) slog.Attr {
	if n == 0 {
		return slog.String("duckdb_max_temp_size", "derived")
	}
	return slog.Int64("duckdb_max_temp_size", n)
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
		spillLogAttr(s.DuckDBMaxTempSize),
	)
}

// channelFields has the fields of Channel without its methods, so that
// formatting it does not call String again.
type channelFields Channel

// redacted reduces the URL to its scheme and host; the files and
// variables only name where a secret is, so they stay.
func (c Channel) redacted() channelFields {
	if c.URL != "" {
		c.URL = RedactChannelURL(c.URL)
	}
	return channelFields(c)
}

// String prints the channel with its URL reduced to scheme and host, for
// %v, %+v and %s.
func (c Channel) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString is String for %#v.
func (c Channel) GoString() string {
	return strings.Replace(fmt.Sprintf("%#v", c.redacted()), "config.channelFields", "config.Channel", 1)
}

// MarshalJSON writes the channel with its URL reduced to scheme and host.
func (c Channel) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

// LogValue logs the channel with its URL reduced to scheme and host and
// each secret source shown as set or not.
func (c Channel) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("type", c.Type),
		slog.String("min_severity", c.MinSeverity),
		slog.Bool("url_set", c.URL != ""),
		slog.Bool("url_file_set", c.URLFile != ""),
		slog.Bool("url_env_set", c.URLEnv != ""),
		slog.Bool("bearer_token_file_set", c.BearerTokenFile != ""),
		slog.Bool("bearer_token_env_set", c.BearerTokenEnv != ""),
	}
	if c.URL != "" {
		attrs = append(attrs, slog.String("url", RedactChannelURL(c.URL)))
	}
	attrs = append(attrs,
		slog.Any("systems", c.Systems),
		slog.Any("checks", c.Checks),
		slog.Any("exclude_checks", c.ExcludeChecks),
		slog.Any("categories", c.Categories),
		slog.Any("kinds", c.Kinds),
		slog.String("repeat_every", c.RepeatEvery.String()),
		slog.String("timeout", c.Timeout.String()),
		slog.String("give_up_after", c.GiveUpAfter.String()),
	)
	return slog.GroupValue(attrs...)
}

// LogValue logs the notifications block, each channel under its name.
func (n Notifications) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("ui_url", n.UIURL)}
	for _, c := range n.Channels {
		attrs = append(attrs, slog.Any(c.Name, c.LogValue()))
	}
	return slog.GroupValue(attrs...)
}

// MarshalYAML writes the channel with its URL reduced to scheme and host.
func (c Channel) MarshalYAML() (any, error) { return c.redacted(), nil }
