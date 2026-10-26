package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/config"
)

// startWarnings runs the start check without event capture and returns
// its log.
func startWarnings(t *testing.T, systems []config.System) string {
	t.Helper()
	var buf bytes.Buffer
	if err := checkSystems(systems, false, slog.New(slog.NewTextHandler(&buf, nil))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// An entry with micro: false and capture off is never dialled; its missing
// file is still warned about, without a claim that it is dialled again.
func TestStartWarnsForAnAccountNeverDialled(t *testing.T) {
	f := false
	out := startWarnings(t, []config.System{{ID: "s", DataDir: t.TempDir(),
		Accounts: []config.Account{{Name: "X", Creds: "/nonexistent/x", Micro: &f}}}})
	if !strings.Contains(out, "account file missing: the account cannot connect until the file can be read") ||
		!strings.Contains(out, "account=X") {
		t.Errorf("no warning for the entry:\n%s", out)
	}
	if strings.Contains(out, "dialled again") {
		t.Errorf("claims a redial for an account that is never dialled:\n%s", out)
	}
}

// A file that exists but cannot be read is warned about like a missing one.
func TestStartWarnsForAnUnreadableAccountFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 000")
	}
	p := filepath.Join(t.TempDir(), "c")
	if err := os.WriteFile(p, []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	out := startWarnings(t, []config.System{{ID: "s", DataDir: t.TempDir(), Accounts: []config.Account{{Name: "X", Creds: p}}}})
	if !strings.Contains(out, "account file missing") || !strings.Contains(out, "permission denied") {
		t.Errorf("no warning with permission denied:\n%s", out)
	}
}
