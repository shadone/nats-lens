package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Shutdown while an account is still being redialled stops within the
// stop deadline, and goroutines of the redial do not outlive it.
func TestShutdownWhileAnAccountRedialsStopsInTime(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	url := refusalServer(t)
	dir := t.TempDir()
	accs := "  - { name: BROKEN, creds: /nonexistent/a.creds }\n  - { name: BROKEN2, creds: /nonexistent/b.creds }\n"
	conf := refusalConfig(t, dir, url, true, accs)
	reg, stop := runWithStatusStop(t, conf)
	waitForEpochs(t, reg, "default", 1)
	// Let the first redial wait (2s) fail once, so the keep loop is in its second wait.
	time.Sleep(3 * time.Second)
	start := time.Now()
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("stop took %v", d)
	}
}

// An account with a CA file that exists but is not a certificate does not stop the start.
func TestAccountTLSFileNotACertificateStartsWithOneNotConnected(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	url := refusalServer(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(bad, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	accs := "  - { name: BADTLS, user: shop, password: shop-test, tls: { ca: " + bad + " } }\n"
	conf := refusalConfig(t, dir, url, true, accs)
	reg, stop := runWithStatusStop(t, conf)
	waitForEpochs(t, reg, "default", 2)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if n := refusalCount(t, filepath.Join(dir, "data"), `SELECT count(*) FROM events WHERE kind='collector.not_connected' AND json_extract_string(raw,'$.connection')='BADTLS'`); n != 1 {
		t.Errorf("not_connected events = %d, want 1", n)
	}
}
