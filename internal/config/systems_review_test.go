package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureWarnings sends the default logger to a buffer for the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func parseError(t *testing.T, body string) error {
	t.Helper()
	_, err := Parse("natslens", []string{"-config", writeFile(t, body)})
	return err
}

func TestSystemsNestedDirs(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, body string
		refused    bool
	}{
		{"archive inside another archive", `
archive_dir: /d
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: b, nats: {url: "nats://b:4222"}, archive_dir: /d/a/sub}
`, true},
		{"archive equal to the root another system nests in", `
archive_dir: /arch
systems:
  - {id: a, nats: {url: "nats://a:4222"}, archive_dir: /arch}
  - {id: b, nats: {url: "nats://b:4222"}}
`, true},
		{"archive inside another data dir", `
data_dir: /data
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: b, nats: {url: "nats://b:4222"}, archive_dir: /data/a/x}
`, true},
		{"data dir inside another archive", `
data_dir: /data
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: b, nats: {url: "nats://b:4222"}, data_dir: /data/a/archive/b, archive_dir: /arch/b}
`, true},
		{"archive inside another archive through a symlink", fmt.Sprintf(`
systems:
  - {id: a, nats: {url: "nats://a:4222"}, archive_dir: %s/x}
  - {id: b, nats: {url: "nats://b:4222"}, archive_dir: %s/x/sub}
`, real, link), true},
		{"data dir inside another data dir", `
data_dir: /data
archive_dir: /arch
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: b, nats: {url: "nats://b:4222"}, data_dir: /data/a/b}
`, false},
		{"sibling with a common name prefix", `
archive_dir: /arch
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: ab, nats: {url: "nats://b:4222"}}
`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseError(t, tc.body)
			switch {
			case tc.refused && err == nil:
				t.Fatal("accepted")
			case tc.refused && !strings.Contains(err.Error(), "directory"):
				t.Fatalf("error %q does not name the directory clash", err)
			case !tc.refused && err != nil:
				t.Fatalf("refused: %v", err)
			}
		})
	}
}

func TestSystemsBadIDsSkipDirectoryCheck(t *testing.T) {
	err := parseError(t, `
systems:
  - {nats: {url: "nats://a:4222"}}
  - {nats: {url: "nats://b:4222"}}
  - {id: x, nats: {url: "nats://x:4222"}}
  - {id: x, nats: {url: "nats://y:4222"}}
`)
	if err == nil {
		t.Fatal("accepted")
	}
	if strings.Contains(err.Error(), "directory") {
		t.Fatalf("missing or duplicate ids also reported as a directory clash: %v", err)
	}
}

func TestSystemsSecretsHidden(t *testing.T) {
	sys := System{
		ID:   "prod",
		NATS: NATS{URL: "nats://u:urlpass@h:4222", Name: "lens", User: "sys", Password: "natspass"},
		Accounts: []Account{
			{Name: "APP", URL: "nats://app:accurlpass@h:4222", User: "app", Password: "accpass"},
		},
	}
	secrets := []string{"urlpass", "natspass", "accurlpass", "accpass"}
	var logged bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logged, nil))
	log.Info("system", "system", sys, "nats", sys.NATS, "account", sys.Accounts[0])
	outputs := map[string]string{"log": logged.String()}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		outputs[verb+" system"] = fmt.Sprintf(verb, sys)
		outputs[verb+" nats"] = fmt.Sprintf(verb, sys.NATS)
		outputs[verb+" account"] = fmt.Sprintf(verb, sys.Accounts[0])
		outputs[verb+" nats pointer"] = fmt.Sprintf(verb, &sys.NATS)
	}
	for what, out := range outputs {
		for _, s := range secrets {
			if strings.Contains(out, s) {
				t.Errorf("%s shows %q: %s", what, s, out)
			}
		}
		if !strings.Contains(out, "h:4222") {
			t.Errorf("%s lost the host: %s", what, out)
		}
	}
}

func TestSystemsPasswordEnvCollision(t *testing.T) {
	cases := map[string]string{
		"across systems": `
systems:
  - {id: a, nats: {url: "nats://a:4222"}, accounts: [{name: b_account_c, user: u}]}
  - {id: a-account-b, nats: {url: "nats://b:4222"}, accounts: [{name: c, user: u}]}
`,
		"accounts in one system": `
systems:
  - {id: a, nats: {url: "nats://a:4222"}, accounts: [{name: a-b, user: u}, {name: a_b, user: v}]}
`,
		"accounts without systems": `
accounts: [{name: a-b, user: u}, {name: a_b, user: v}]
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := parseError(t, body)
			if err == nil || !strings.Contains(err.Error(), "_PASSWORD") {
				t.Fatalf("collision not refused: %v", err)
			}
		})
	}
	if err := parseError(t, `
systems:
  - {id: a, nats: {url: "nats://a:4222"}, accounts: [{name: a-b, creds: /c}, {name: a_b, creds: /d}]}
`); err != nil {
		t.Fatalf("accounts without a user read no variable: %v", err)
	}
}

func TestSystemsMemoryShare(t *testing.T) {
	logs := captureWarnings(t)
	cfg := parseFile(t, `
duckdb_memory_limit: 3GiB
systems:
  - {id: a, nats: {url: "nats://a:4222"}, duckdb_memory_limit: 1GiB}
  - {id: b, nats: {url: "nats://b:4222"}}
  - {id: c, nats: {url: "nats://c:4222"}}
`)
	sys := cfg.ResolvedSystems()
	if sys[0].DuckDBMemoryLimit != 1<<30 || sys[1].DuckDBMemoryLimit != 1<<30 || sys[2].DuckDBMemoryLimit != 1<<30 {
		t.Errorf("limits %d %d %d", sys[0].DuckDBMemoryLimit, sys[1].DuckDBMemoryLimit, sys[2].DuckDBMemoryLimit)
	}
	if logs.Len() != 0 {
		t.Errorf("unexpected warning: %s", logs)
	}
	cfg = parseFile(t, `
duckdb_memory_limit: 1GiB
systems:
  - {id: a, nats: {url: "nats://a:4222"}, duckdb_memory_limit: 2GiB}
  - {id: b, nats: {url: "nats://b:4222"}}
`)
	if got := cfg.ResolvedSystems()[1].DuckDBMemoryLimit; got != MinSystemMemoryLimit {
		t.Errorf("b gets %d, want the floor", got)
	}
	if !strings.Contains(logs.String(), "duckdb_memory_limit") {
		t.Errorf("no warning for explicit limits over the budget: %q", logs)
	}
}

func TestSystemsEmptyRefused(t *testing.T) {
	for _, body := range []string{"systems: []\n", "systems:\n"} {
		if err := parseError(t, body); err == nil || !strings.Contains(err.Error(), "systems is empty") {
			t.Errorf("%q: %v", body, err)
		}
	}
}

func TestSystemsWarnAboutLegacyPasswordVariables(t *testing.T) {
	t.Setenv(PasswordEnv, "x")
	t.Setenv(AccountPasswordEnv("APP"), "y")
	logs := captureWarnings(t)
	parseFile(t, "systems:\n  - {id: a, nats: {url: \"nats://a:4222\"}}\n")
	out := logs.String()
	if strings.Count(out, "level=WARN") != 1 || !strings.Contains(out, PasswordEnv) || !strings.Contains(out, "NATSLENS_ACCOUNT_APP_PASSWORD") {
		t.Fatalf("want one warning naming both variables: %q", out)
	}
	if strings.Contains(out, "=x") || strings.Contains(out, "=y") {
		t.Fatalf("warning shows a value: %q", out)
	}
	logs.Reset()
	parseFile(t, "data_dir: ./data\n")
	if logs.Len() != 0 {
		t.Fatalf("warning without systems: %q", logs)
	}
}
