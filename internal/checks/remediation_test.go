package checks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/config"
)

func writeRemediation(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "remediation.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A remediation file's text follows the check's own remediation, after a
// separator line; checks it does not name keep theirs.
func TestRemediationFileAppends(t *testing.T) {
	base, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	p := writeRemediation(t, "stream-011: >-\n  Ask the platform team\n  before recreating it.\nsystem-001: Page the on-call.\n")
	set, err := Load(config.Checks{RemediationFile: p})
	if err != nil {
		t.Fatal(err)
	}
	for code, extra := range map[string]string{
		"stream-011": "Ask the platform team before recreating it.",
		"system-001": "Page the on-call.",
	} {
		before, _ := base.Lookup(code)
		after, _ := set.Lookup(code)
		if want := before.Remediation + RemediationSeparator + extra; after.Remediation != want {
			t.Errorf("%s remediation = %q, want %q", code, after.Remediation, want)
		}
		if before.Remediation == after.Remediation {
			t.Errorf("%s: the loaded set shares its check with another set", code)
		}
	}
	before, _ := base.Lookup("stream-012")
	after, _ := set.Lookup("stream-012")
	if after.Remediation != before.Remediation {
		t.Errorf("stream-012 changed: %q", after.Remediation)
	}
	if n := set.RemediationExtended(); n != 2 {
		t.Errorf("RemediationExtended = %d, want 2", n)
	}
	for _, c := range set.Checks() {
		if l, _ := set.Lookup(c.Code); l != c {
			t.Errorf("%s: Checks and Lookup disagree", c.Code)
		}
	}
}

// An unknown code in the remediation file stops the start, as an unknown
// code in checks.disabled does.
func TestRemediationFileRejectsUnknownCode(t *testing.T) {
	p := writeRemediation(t, "stream-999: Nobody knows this check.\n")
	_, err := Load(config.Checks{RemediationFile: p})
	if err == nil || !strings.Contains(err.Error(), `unknown check "stream-999"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemediationFileErrors(t *testing.T) {
	for name, tc := range map[string]struct{ path, err string }{
		"missing":   {filepath.Join(t.TempDir(), "none.yaml"), "no such file"},
		"not a map": {writeRemediation(t, "- stream-011\n"), "checks.remediation_file"},
		"empty":     {writeRemediation(t, "stream-011: \"\"\n"), "stream-011: empty text"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(config.Checks{RemediationFile: tc.path})
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("err = %v, want %q", err, tc.err)
			}
		})
	}
}

func TestRemediationPlaybook(t *testing.T) {
	for _, url := range []string{"https://runbooks.example.com/nats/durable-missing", "http://example.com", "javascript:alert(1)", "https:///missing", "https://user:password@example.com", "//example.com", "https://example.com/\nunsafe", "\t", "https://example.com/\t", "data:text/html,unsafe"} {
		t.Run(url, func(t *testing.T) {
			body := "consumer-010:\n  text: Read the playbook first.\n  playbook: " + strconv.Quote(url) + "\n"
			set, err := Load(config.Checks{RemediationFile: writeRemediation(t, body)})
			if url != "https://runbooks.example.com/nats/durable-missing" {
				if err == nil {
					t.Fatal("accepted unsafe playbook")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c, _ := set.Lookup("consumer-010")
			if c.Playbook != url {
				t.Fatalf("playbook = %q", c.Playbook)
			}
			if !strings.HasSuffix(c.Remediation, "Read the playbook first.") {
				t.Fatal(c.Remediation)
			}
		})
	}
}
