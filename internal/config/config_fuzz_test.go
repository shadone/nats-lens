package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FuzzParseConfigFile reads YAML configuration files: a file is accepted
// or refused with an error, the same way every time; a refused file returns
// the base unchanged, and no file, accepted or refused, changes the base it
// was read over. Validate is left out, as it looks at the data directories
// on disk.
func FuzzParseConfigFile(f *testing.F) {
	example, err := os.ReadFile(filepath.Join("..", "..", "natslens.example.yaml"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(example)
	for _, s := range []string{
		"", "{}", "null", "---\n---\n", "interval: 30s\n", "interval: 213504d\n", "retention: -1d\n",
		"interval: [1, 2]\n", "unknown_key: 1\n", "systems: []\n", "systems:\n  - id: a\n    nats: {url: nats://x}\n",
		"systems:\n  - id: a\nnats: {url: nats://x}\n", "checks:\n  thresholds:\n    stream-001: {limit: '0.5'}\n",
		"checks:\n  thresholds:\n    stream-001: {other: '1'}\n", "checks: {loopback_is_tooling: true}\n",
		"checks: {disabled: [a, b]}\n", "accounts: [{name: B}]\n", "a: &x [*x, *x]\n", "interval: &d 1s\ntimeout: *d\n",
		"\ufeffinterval: 1s\n", "interval:\t1s\n", "duckdb_memory_limit: 1e99GiB\n", "duckdb_memory_limit: 18446744073709551616\n",
		"connz_limit: 99999999999999999999\n", "log_level: \"\\u0000\"\n", "listen: \"[::1]:8080\"\n",
		"notifications:\n  channels:\n    - name: x\n      url: http://127.0.0.1:1/\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8<<10 {
			return
		}
		base := fuzzBase()
		cfg, err := decodeFile("fuzz.yaml", data, base)
		if !reflect.DeepEqual(base, fuzzBase()) {
			t.Fatalf("reading %q changed the base configuration", data)
		}
		again, againErr := decodeFile("fuzz.yaml", data, fuzzBase())
		if (err == nil) != (againErr == nil) || (err != nil && err.Error() != againErr.Error()) || !reflect.DeepEqual(cfg, again) {
			t.Fatalf("reading %q is not deterministic: %v / %v", data, err, againErr)
		}
		if err != nil {
			if !reflect.DeepEqual(cfg, fuzzBase()) {
				t.Fatalf("refused file %q returned a changed configuration: %v", data, err)
			}
			return
		}
		_ = cfg.LogValue().Resolve().Group()
		_ = slog.AnyValue(cfg).String()
	})
}
