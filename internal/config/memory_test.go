package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDuckDBMemoryLimit(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := cgroupMemoryFiles
	defer func() { cgroupMemoryFiles = old }()

	cgroupMemoryFiles = []string{write("v2", "2147483648\n")}
	if got := (Config{}).DuckDBMemoryLimit(); got != 2147483648*6/10 {
		t.Fatalf("cgroup limit: %d", got)
	}
	cgroupMemoryFiles = []string{write("max", "max\n")}
	if got := (Config{}).DuckDBMemoryLimit(); got != 1<<30 {
		t.Fatalf("no cgroup limit: %d", got)
	}
	cgroupMemoryFiles = []string{filepath.Join(dir, "missing")}
	if got := (Config{}).DuckDBMemoryLimit(); got != 1<<30 {
		t.Fatalf("no cgroup: %d", got)
	}
	p := writeFile(t, "duckdb_memory_limit: 512MiB\n")
	cfg, err := LoadFile(p, Default())
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.DuckDBMemoryLimit(); got != 512<<20 {
		t.Fatalf("configured: %d", got)
	}
	var b ByteSize
	for _, bad := range []string{"1GB", "-1", "x"} {
		if err := b.UnmarshalText([]byte(bad)); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

// The spill bound is duckdb_max_temp_size, else twice the memory limit;
// a listed system takes its own, else the top-level value, else twice its
// own memory limit.
func TestDuckDBMaxTempSize(t *testing.T) {
	if got := (Config{DuckDBMemory: 300 << 20}).DuckDBMaxTempSize(); got != 600<<20 {
		t.Fatalf("default: %d", got)
	}
	if got := (Config{DuckDBMemory: 300 << 20, DuckDBMaxTemp: 1 << 30}).DuckDBMaxTempSize(); got != 1<<30 {
		t.Fatalf("configured: %d", got)
	}
	cfg := parseFile(t, `duckdb_memory_limit: 1GiB
systems:
  - id: a
    nats: { url: "nats://a:4222" }
    duckdb_max_temp_size: 3GiB
  - id: b
    nats: { url: "nats://b:4222" }
    duckdb_memory_limit: 256MiB
  - id: c
    nats: { url: "nats://c:4222" }
`)
	want := map[string]int64{"a": 3 << 30, "b": 512 << 20, "c": 2 * ((1<<30 - 256<<20) / 2)}
	for _, s := range cfg.ResolvedSystems() {
		if s.DuckDBMaxTempSize != want[s.ID] {
			t.Errorf("%s: %d, want %d", s.ID, s.DuckDBMaxTempSize, want[s.ID])
		}
	}
	cfg.DuckDBMaxTemp = 5 << 30
	want = map[string]int64{"a": 3 << 30, "b": 5 << 30, "c": 5 << 30}
	for _, s := range cfg.ResolvedSystems() {
		if s.DuckDBMaxTempSize != want[s.ID] {
			t.Errorf("top-level set, %s: %d, want %d", s.ID, s.DuckDBMaxTempSize, want[s.ID])
		}
	}
}
