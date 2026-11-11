package config

import (
	"reflect"
	"testing"
)

// The file is decoded into a copy of the base: thresholds and
// loopback_is_tooling in the file leave the base's map and pointer as they
// were, also when the file is then refused.
func TestDecodeFileLeavesTheBaseAlone(t *testing.T) {
	for _, body := range []string{
		"checks:\n  thresholds:\n    stream-001: {limit: '0.5', other: '1'}\n    server-002: {x: '2'}\n  loopback_is_tooling: true\n",
		"checks:\n  thresholds:\n    stream-001: {limit: '0.5'}\nsystems: []\n",
	} {
		base := fuzzBase()
		cfg, err := decodeFile("base.yaml", []byte(body), base)
		if !reflect.DeepEqual(base, fuzzBase()) {
			t.Fatalf("decoding %q changed the base: thresholds %v, loopback %v", body, base.Checks.Thresholds, *base.Checks.LoopbackIsTooling)
		}
		if err == nil && (cfg.Checks.Thresholds["stream-001"]["limit"] != "0.5" || !cfg.Checks.LoopbackTooling()) {
			t.Fatalf("decoding %q lost the file's values: %v", body, cfg.Checks)
		}
	}
}

// fuzzBase is a base configuration with a map, a slice and a pointer set,
// so that a decode that writes through to the base shows.
func fuzzBase() Config {
	c := Default()
	c.Checks.Thresholds = map[string]map[string]string{"stream-001": {"limit": "0.9"}}
	c.Checks.Disabled = []string{"server-001"}
	loopback := false
	c.Checks.LoopbackIsTooling = &loopback
	c.Accounts = []Account{{Name: "APP"}}
	return c
}
