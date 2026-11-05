package checks

import (
	"testing"

	"github.com/shadone/nats-lens/internal/config"
)

// libConn stores a connection with a client library.
func (f *fx) libConn(epoch int64, id string, cid int64, name, ip, lang, version string) {
	f.t.Helper()
	row := m{"epoch": epoch, "server_id": id, "cid": cid, "ip": ip}
	if name != "" {
		row["name"] = name
	}
	if lang != "" {
		row["lang"] = lang
	}
	if version != "" {
		row["version"] = version
	}
	f.row("connections", row)
}

// An application with connections older than its language's minimum fires
// one info finding; versions compare as numbers (2.10.0 is newer than
// 2.9.0), a version at the minimum is not old, a suffix after the numbers
// is ignored, and languages without a minimum or connections without a
// version are not judged.
func TestOutdatedClientLibrary(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.libConn(40, "S0", 1, "resize-worker@rw-a", "10.0.0.1", "go", "1.31.0")
	f.libConn(40, "S1", 2, "resize-worker@rw-b", "10.0.0.2", "go", "1.31.0")
	f.libConn(40, "S1", 3, "resize-worker@rw-c", "10.0.0.3", "go", "1.54.0")
	f.libConn(40, "S0", 4, "catalog-api@ca-a", "10.0.0.4", "nats.js", "3.0.2")
	f.libConn(40, "S0", 5, "order-service", "10.0.0.5", "go", "1.37.0")
	f.libConn(40, "S0", 6, "ledger", "10.0.0.6", "go", "1.36.9")
	f.libConn(40, "S2", 7, "invoice-mailer", "10.0.0.7", "python3", "2.10.0")
	f.libConn(40, "S2", 8, "stock-sync", "10.0.0.8", "java", "2.20.0-SNAPSHOT")
	f.libConn(40, "S2", 9, "label-printer", "10.0.0.9", ".NET", "2.4.1+3f2a9c1")
	f.libConn(40, "S2", 10, "edge-app", "10.0.0.10", "nats.deno", "1.0.0")
	f.libConn(40, "S2", 11, "no-version", "10.0.0.11", "go", "")
	f.libConn(40, "S2", 12, "", "10.0.0.12", "rust", "0.30.0")
	r := f.fires("client-008", 40, "resize-worker", "ledger", "label-printer", "(unnamed)")
	want(t, r["resize-worker"], "info",
		"2 of 3 connections use a client library older than its minimum: go 1.31.0 (minimum 1.37.0) (nats-0, nats-1).")
	if v := r["resize-worker"].Value; v == nil || *v != 2 {
		t.Errorf("value %v, want 2", v)
	}
	want(t, r["ledger"], "info", "1 of 1 connection uses a client library older than its minimum: go 1.36.9 (minimum 1.37.0) (nats-0).")
	want(t, r["label-printer"], "info", ".NET 2.4.1+3f2a9c1 (minimum 2.5.0)")
	want(t, r["(unnamed)"], "info", "rust 0.30.0 (minimum 0.37.0)")
	// A minimum of 0 judges no client of that language; a higher one is tunable.
	if got := f.eval("client-008", 40, map[string]string{"min_go": "0"}); len(got) != 2 {
		t.Errorf("with min_go 0: %v", rowsText(got))
	}
	if got := f.eval("client-008", 40, map[string]string{"min_nats_js": "3.1"}); len(got) != 5 {
		t.Errorf("with min_nats_js 3.1: %v", rowsText(got))
	}
}

// An old connection seen within the lookback keeps the finding, described
// from the newest collection that had one; one seen only before the
// lookback does not count.
func TestOutdatedClientLibraryLookback(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	// The lookback of epoch 200 starts at epoch 80.
	f.libConn(100, "S0", 1, "report-builder", "10.0.0.1", "go", "1.20.0")
	f.libConn(110, "S0", 2, "report-builder", "10.0.0.1", "go", "1.20.0")
	f.libConn(110, "S1", 3, "report-builder", "10.0.0.2", "go", "1.54.0")
	f.libConn(79, "S0", 4, "nightly-export", "10.0.0.3", "go", "1.20.0")
	f.libConn(200, "S0", 5, "report-builder", "10.0.0.1", "go", "1.54.0")
	r := f.fires("client-008", 200, "report-builder")
	want(t, r["report-builder"], "info",
		"In the collection of 09:54:30 UTC, 1 of 2 connections uses a client library older than its minimum: go 1.20.0 (minimum 1.37.0) (nats-0).")
	f.libConn(80, "S0", 6, "nightly-export", "10.0.0.3", "go", "1.20.0")
	f.fires("client-008", 200, "report-builder", "nightly-export")
}

// Tooling (nats-lens's own connections and a CLI on a loopback address) is
// left out unless checks.include_tooling is set.
func TestOutdatedClientLibraryTooling(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.libConn(40, "S0", 1, "natslens-events", "10.0.0.1", "go", "1.20.0")
	f.libConn(40, "S0", 2, "NATS CLI Version 0.1.0", "127.0.0.1", "go", "1.20.0")
	if got := evalTooling(t, f, "client-008", 40, config.Checks{}); len(got) != 0 {
		t.Errorf("tooling counted: %v", rowsText(got))
	}
	if got := evalTooling(t, f, "client-008", 40, config.Checks{IncludeTooling: true}); len(got) != 2 {
		t.Errorf("with tooling: %v", rowsText(got))
	}
}

// A version that does not start with a number ("dev", " 1.2.0") is not
// judged and does not stop the check for the other applications; nats.py
// without package metadata reports 0.0.0, which is not judged either.
func TestOutdatedClientLibraryUnknownVersions(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.libConn(40, "S0", 1, "good-old", "10.0.0.1", "go", "1.10.0")
	f.libConn(40, "S0", 2, "dev-build", "10.0.0.2", "go", "dev")
	f.libConn(40, "S0", 3, "unknown-build", "10.0.0.3", "go", "unknown")
	f.libConn(40, "S0", 4, "spaced", "10.0.0.4", "go", " 1.2.0")
	f.libConn(40, "S0", 5, "py-no-metadata", "10.0.0.5", "python3", "0.0.0")
	f.libConn(40, "S0", 6, "py-old", "10.0.0.6", "python3", "2.1.0")
	f.libConn(40, "S0", 7, "huge", "10.0.0.7", "go", "12345678901.0.0")
	f.libConn(40, "S0", 8, "go-zero", "10.0.0.8", "go", "0.0.0")
	f.fires("client-008", 40, "good-old", "py-old", "go-zero")
}
