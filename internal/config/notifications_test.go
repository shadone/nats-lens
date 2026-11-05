package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const notifySystems = `
systems:
  - id: example-prod
    nats: {url: "nats://127.0.0.1:4222"}
  - id: example-staging
    nats: {url: "nats://127.0.0.1:4223"}
`

func notifyConfig(channels string) string {
	return notifySystems + "notifications:\n  ui_url: https://nats-lens.example.com\n  channels:\n" + channels
}

func TestNotificationsDefaults(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg := parseFile(t, notifyConfig(`
    - name: ops-webhook
      type: webhook
      url_file: /var/run/secrets/natslens/ops-webhook-url
    - name: ops-chat
      type: slack
      url_env: NATSLENS_OPS_CHAT_URL
      min_severity: critical
      systems: [example-prod]
      kinds: [firing, resolved]
      repeat_every: 2h
      timeout: 30s
      give_up_after: 1h
`))
	n := cfg.Notifications
	if n.UIURL != "https://nats-lens.example.com" || len(n.Channels) != 2 {
		t.Fatalf("notifications %+v", n)
	}
	w := n.Channels[0]
	if w.MinSeverity != "warning" || w.Timeout.D() != 10*time.Second || w.GiveUpAfter.D() != 24*time.Hour ||
		w.RepeatEvery != 0 || len(w.Kinds) != 0 {
		t.Errorf("webhook defaults %+v", w)
	}
	for _, k := range ChannelKinds {
		if !w.Sends(k) {
			t.Errorf("default kinds lack %s", k)
		}
	}
	s := n.Channels[1]
	if s.MinSeverity != "critical" || s.Timeout.D() != 30*time.Second || s.GiveUpAfter.D() != time.Hour ||
		s.RepeatEvery.D() != 2*time.Hour {
		t.Errorf("slack values %+v", s)
	}
	if s.Sends("changed") || !s.Sends("resolved") || !s.Sends("test") {
		t.Errorf("kinds filter: changed %v resolved %v test %v", s.Sends("changed"), s.Sends("resolved"), s.Sends("test"))
	}
}

func TestNoNotificationsIsValid(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg := parseFile(t, "data_dir: /tmp/x\n")
	if len(cfg.Notifications.Channels) != 0 {
		t.Fatalf("channels %+v", cfg.Notifications.Channels)
	}
}

// Every refusal of the block stops the start with a message that names the
// channel and the key.
func TestNotificationsRefusals(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	base := "    - name: ops-webhook\n      type: webhook\n"
	cases := []struct{ name, channels, want string }{
		{"no destination", base, "exactly one of url, url_file and url_env"},
		{"two destinations", base + "      url: https://hooks.example.com/x\n      url_env: X\n", "exactly one of url"},
		{"three destinations", base + "      url: https://hooks.example.com/x\n      url_env: X\n      url_file: /f\n", "exactly one of url"},
		{"two tokens", base + "      url_env: X\n      bearer_token_file: /t\n      bearer_token_env: T\n", "at most one of bearer_token_file"},
		{"slack token", "    - name: ops-chat\n      type: slack\n      url_env: X\n      bearer_token_env: T\n", "webhook channel only"},
		{"bad type", "    - name: ops-mail\n      type: email\n      url_env: X\n", `type "email"`},
		{"bad name", "    - name: Ops_Hook\n      type: webhook\n      url_env: X\n", `name "Ops_Hook" must match`},
		{"empty name", "    - type: webhook\n      url_env: X\n", `name "" must match`},
		{"duplicate name", base + "      url_env: X\n" + base + "      url_env: Y\n", "duplicate name"},
		{"bad severity", base + "      url_env: X\n      min_severity: major\n", `min_severity "major"`},
		{"unknown system", base + "      url_env: X\n      systems: [example-dev]\n", `unknown system "example-dev"`},
		{"bad kind", base + "      url_env: X\n      kinds: [firing, paged]\n", `unknown kind "paged"`},
		{"short repeat", base + "      url_env: X\n      repeat_every: 30m\n", "repeat_every must be 0"},
		{"negative repeat", base + "      url_env: X\n      repeat_every: -1h\n", "repeat_every must be 0"},
		{"zero timeout", base + "      url_env: X\n      timeout: 0s\n", "timeout must be positive and at most 60s"},
		{"long timeout", base + "      url_env: X\n      timeout: 61s\n", "timeout must be positive and at most 60s"},
		{"zero give up", base + "      url_env: X\n      give_up_after: 0s\n", "give_up_after must be positive"},
		{"http url", base + "      url: http://hooks.example.com/x\n", "http URL is accepted only for a loopback host"},
		{"ftp url", base + "      url: ftp://hooks.example.com/x\n", "the URL must be https"},
		{"relative url", base + "      url: hooks.example.com/x\n", "does not parse as an absolute URL"},
		{"unknown key", base + "      url_env: X\n      headers: {X-A: b}\n", "headers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse("natslens", []string{"-config", writeFile(t, notifyConfig(c.channels))})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
			if c.name != "unknown key" && c.name != "empty name" && !strings.Contains(err.Error(), "notifications.channels[") {
				t.Errorf("error does not name the channel: %v", err)
			}
		})
	}
}

func TestNotificationsBadUIURL(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	for _, u := range []string{"nats-lens.example.com", "ftp://nats-lens.example.com", "https://"} {
		body := "notifications:\n  ui_url: " + u + "\n"
		if _, err := Parse("natslens", []string{"-config", writeFile(t, body)}); err == nil ||
			!strings.Contains(err.Error(), "notifications.ui_url") {
			t.Errorf("ui_url %q: err %v", u, err)
		}
	}
}

// http is accepted for a loopback host, for tests; https anywhere.
func TestNotificationsURLs(t *testing.T) {
	for _, u := range []string{"https://hooks.example.com/x", "http://127.0.0.1:9000/x", "http://localhost/x", "http://[::1]:80/x"} {
		if err := CheckChannelURL(u); err != nil {
			t.Errorf("%s refused: %v", u, err)
		}
	}
	// A refusal never prints the URL, which can be a secret.
	secret := "http://hooks.example.com/services/T000/B000/s3cr3tpath?token=q5ery"
	err := CheckChannelURL(secret)
	if err == nil {
		t.Fatal("http on a remote host accepted")
	}
	for _, part := range []string{"s3cr3tpath", "q5ery", "/services/"} {
		if strings.Contains(err.Error(), part) {
			t.Errorf("error holds %q: %v", part, err)
		}
	}
}

// The single-system form also takes the block, and its one system is the
// default id.
func TestNotificationsSingleSystem(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg := parseFile(t, "notifications:\n  channels:\n    - name: ops\n      type: webhook\n      url_env: X\n      systems: [default]\n")
	if len(cfg.Notifications.Channels) != 1 {
		t.Fatal("channel lost")
	}
}

// notifications is shared, like checks: a systems entry cannot set it.
func TestNotificationsRefusedInSystem(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	body := "systems:\n  - id: example-prod\n    nats: {url: \"nats://127.0.0.1:4222\"}\n    notifications:\n      channels: []\n"
	if _, err := Parse("natslens", []string{"-config", writeFile(t, body)}); err == nil ||
		!strings.Contains(err.Error(), "notifications") {
		t.Fatalf("err %v", err)
	}
}

func TestChannelAccepts(t *testing.T) {
	c := DefaultChannel()
	c.Systems = []string{"example-prod"}
	c.ExcludeChecks = []string{"server-005"}
	c.Categories = []string{"health", "storage"}
	cases := []struct {
		system, check, category, severity string
		want                              bool
	}{
		{"example-prod", "stream-001", "health", "critical", true},
		{"example-prod", "stream-001", "health", "warning", true},
		{"example-prod", "stream-001", "health", "info", false},
		{"example-staging", "stream-001", "health", "critical", false},
		{"example-prod", "server-005", "health", "critical", false},
		{"example-prod", "client-001", "clients", "critical", false},
	}
	for _, x := range cases {
		if got := c.Accepts(x.system, x.check, x.category, x.severity); got != x.want {
			t.Errorf("%+v: got %v", x, got)
		}
	}
	c.Checks = []string{"stream-002"}
	if c.Accepts("example-prod", "stream-001", "health", "critical") {
		t.Error("checks filter ignored")
	}
}

// secretConfig has a channel whose plain URL holds a token in its path and
// query, and a webhook whose token is in a file and a variable.
const (
	secretPath  = "T0SECRETPATH"
	secretQuery = "Q0SECRETQUERY"
)

func secretConfig() string {
	return notifyConfig(fmt.Sprintf(`
    - name: ops-chat
      type: slack
      url: https://hooks.example.com/services/%s?sig=%s
    - name: ops-webhook
      type: webhook
      url_file: /var/run/secrets/natslens/ops-webhook-url
      bearer_token_env: NATSLENS_OPS_TOKEN
`, secretPath, secretQuery))
}

// The starting line names each channel and the receiver's host, never the
// URL's path or query.
func TestNotificationsStartLogRedacted(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg := parseFile(t, secretConfig())
	var text, js bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("starting", "config", cfg)
	slog.New(slog.NewJSONHandler(&js, nil)).Info("starting", "config", cfg)
	for _, out := range []string{text.String(), js.String()} {
		for _, part := range []string{secretPath, secretQuery, "/services/"} {
			if strings.Contains(out, part) {
				t.Errorf("start log holds %q: %s", part, out)
			}
		}
		for _, want := range []string{"ops-chat", "ops-webhook", "hooks.example.com", "bearer_token_env_set"} {
			if !strings.Contains(out, want) {
				t.Errorf("start log lacks %q: %s", want, out)
			}
		}
	}
	// Printing the configuration or a channel any other way hides the
	// path and query too.
	ch := cfg.Notifications.Channels[0]
	b, err := json.Marshal(cfg.Notifications)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprint(ch), fmt.Sprintf("%+v", ch), fmt.Sprintf("%#v", ch), fmt.Sprintf("%v", cfg.Notifications), string(b)} {
		for _, part := range []string{secretPath, secretQuery} {
			if strings.Contains(out, part) {
				t.Errorf("printed channel holds %q: %s", part, out)
			}
		}
		if !strings.Contains(out, "hooks.example.com") {
			t.Errorf("printed channel lacks the host: %s", out)
		}
	}
}

func TestRedactChannelURL(t *testing.T) {
	cases := map[string]string{
		"https://hooks.example.com/services/a/b?c=d": "https://hooks.example.com",
		"https://u:p@hooks.example.com:8443/x":       "https://hooks.example.com:8443",
		"not a url":                                  "[invalid]",
		"%zz":                                        "[invalid]",
	}
	for in, want := range cases {
		if got := RedactChannelURL(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
