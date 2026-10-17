package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordEnvCollisionCountsOnlyVariablesRead(t *testing.T) {
	accepted := map[string]string{
		"legacy accounts with passwords in the file": `
accounts: [{name: a-b, user: u, password: p1}, {name: a_b, user: v, password: p2}]
`,
		"system nats password in the file": `
systems:
  - {id: a-account-b, nats: {url: "nats://a:4222", user: u, password: p}}
  - {id: a, nats: {url: "nats://b:4222"}, accounts: [{name: b_nats, user: v}]}
`,
	}
	for name, body := range accepted {
		t.Run(name, func(t *testing.T) {
			if err := parseError(t, body); err != nil {
				t.Fatalf("refused although no variable is read: %v", err)
			}
		})
	}
	err := parseError(t, `
systems:
  - {id: a-account-b, nats: {url: "nats://a:4222", user: u}}
  - {id: a, nats: {url: "nats://b:4222"}, accounts: [{name: b_nats, user: v}]}
`)
	if err == nil || !strings.Contains(err.Error(), "NATSLENS_SYSTEM_A_ACCOUNT_B_NATS_PASSWORD") ||
		!strings.Contains(err.Error(), "differ in letters or digits, or set password in the file") {
		t.Fatalf("collision not refused with its fix: %v", err)
	}
}

func TestSystemsMemoryFloorOvershootWarned(t *testing.T) {
	logs := captureWarnings(t)
	parseFile(t, `
duckdb_memory_limit: 1GiB
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: b, nats: {url: "nats://b:4222"}}
  - {id: c, nats: {url: "nats://c:4222"}}
`)
	if logs.Len() != 0 {
		t.Fatalf("warning within the budget: %s", logs)
	}
	parseFile(t, `
duckdb_memory_limit: 512MiB
systems:
  - {id: a, nats: {url: "nats://a:4222"}}
  - {id: b, nats: {url: "nats://b:4222"}}
  - {id: c, nats: {url: "nats://c:4222"}}
`)
	if out := logs.String(); !strings.Contains(out, "duckdb_memory_limit") || !strings.Contains(out, "floor") {
		t.Fatalf("want a warning naming the floor: %q", out)
	}
	logs.Reset()
	parseFile(t, `
duckdb_memory_limit: 1GiB
systems:
  - {id: a, nats: {url: "nats://a:4222"}, duckdb_memory_limit: 2GiB}
`)
	if out := logs.String(); !strings.Contains(out, "duckdb_memory_limit") || strings.Contains(out, "floor") {
		t.Fatalf("want a warning about the own limits only: %q", out)
	}
}

func TestSystemsEqualDataDirsRefused(t *testing.T) {
	err := parseError(t, `
systems:
  - {id: a, nats: {url: "nats://a:4222"}, data_dir: /data/x, archive_dir: /arch/a}
  - {id: b, nats: {url: "nats://b:4222"}, data_dir: /data/x, archive_dir: /arch/b}
`)
	if err == nil || !strings.Contains(err.Error(), "same directory") {
		t.Fatalf("two systems with one data_dir: %v", err)
	}
}

func TestSystemsDirsDotDotAfterSymlink(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "real", "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(inner, link); err != nil {
		t.Fatal(err)
	}
	// root/link/../x is root/real/x for the kernel, root/x when cleaned
	// by the letters.
	err := parseError(t, fmt.Sprintf(`
systems:
  - {id: a, nats: {url: "nats://a:4222"}, archive_dir: %s/real/x}
  - {id: b, nats: {url: "nats://b:4222"}, archive_dir: %s/link/../x/sub}
`, root, root))
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("archive nested through link/.. accepted: %v", err)
	}
}

func TestSystemsDirsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "data")); err != nil {
		t.Skip("case-sensitive file system")
	}
	err := parseError(t, fmt.Sprintf(`
systems:
  - {id: a, nats: {url: "nats://a:4222"}, data_dir: %s/Data, archive_dir: %s/arch-a}
  - {id: b, nats: {url: "nats://b:4222"}, archive_dir: %s/data/sub}
`, root, root, root))
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("archive inside a data dir spelled in another case accepted: %v", err)
	}
	err = parseError(t, fmt.Sprintf(`
systems:
  - {id: a, nats: {url: "nats://a:4222"}, data_dir: %s/a, archive_dir: %s/Data}
  - {id: b, nats: {url: "nats://b:4222"}, data_dir: %s/b, archive_dir: %s/data}
`, root, root, root, root))
	if err == nil || !strings.Contains(err.Error(), "same directory") {
		t.Fatalf("one archive spelled in two cases accepted: %v", err)
	}
}

func TestJSONHidesPasswords(t *testing.T) {
	n := NATS{URL: "nats://u:urlpass@h:4222", User: "sys", Password: "natspass"}
	a := Account{Name: "APP", URL: "nats://app:accurlpass@h:4222", User: "app", Password: "accpass"}
	sys := System{ID: "prod", NATS: n, Accounts: []Account{a}}
	cfg := Config{NATS: n, Accounts: []Account{a}, Systems: []SystemConfig{{ID: "prod", NATS: n, Accounts: []Account{a}}}}
	for what, v := range map[string]any{"nats": n, "nats pointer": &n, "account": a, "system": sys, "config": cfg} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		for _, s := range []string{"urlpass", "natspass", "accurlpass", "accpass"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s JSON shows %q: %s", what, s, b)
			}
		}
		if !strings.Contains(string(b), "h:4222") {
			t.Errorf("%s JSON lost the host: %s", what, b)
		}
	}
}

func TestRedactURLUnparsable(t *testing.T) {
	for _, in := range []string{"nats://u:sekret%zz@h", "nats://u:sekret@[::1", "u:sekret@h:4222", "nats://a:4222, u:sekret@h:4222"} {
		out := redactURL(in)
		if strings.Contains(out, "sekret") {
			t.Errorf("%q -> %q", in, out)
		}
		if !strings.Contains(out, "@") {
			t.Errorf("%q -> %q lost the host part", in, out)
		}
	}
	if got := redactURL("nats://a:4222,nats://b:4222"); got != "nats://a:4222,nats://b:4222" {
		t.Errorf("plain list changed: %q", got)
	}
}
