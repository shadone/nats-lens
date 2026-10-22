package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// An account asks its micro services unless it sets micro: false, at the
// top level and in a system's list; the start log says which.
func TestAccountMicro(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	p := writeFile(t, `
data_dir: /var/lib/natslens
nats: { url: nats://sys-host:4222 }
accounts:
  - name: APP
  - name: OPS
    micro: false
  - name: SHOP
    micro: true
`)
	cfg, err := Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatal(err)
	}
	var got []bool
	for _, a := range cfg.Accounts {
		got = append(got, a.AsksMicro())
	}
	if len(got) != 3 || !got[0] || got[1] || !got[2] {
		t.Errorf("asks micro = %v, want [true false true]", got)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("accounts", "accounts", accountsLogValue(cfg.Accounts))
	if !strings.Contains(buf.String(), `"OPS":{"url":"","creds":"","user":"","password_set":false,"tls_ca":"","micro":false}`) {
		t.Errorf("log: %s", buf.String())
	}

	p = writeFile(t, `
data_dir: /var/lib/natslens
systems:
  - id: shop-prod
    nats: { url: nats://sys-host:4222 }
    accounts:
      - name: APP
        micro: false
`)
	cfg, err = Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatal(err)
	}
	if one := cfg.ForSystem(cfg.ResolvedSystems()[0]); len(one.Accounts) != 1 || one.Accounts[0].AsksMicro() {
		t.Errorf("system accounts = %+v", one.Accounts)
	}
}
