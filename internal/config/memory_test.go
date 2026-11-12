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

// The spill bound is duckdb_max_temp_size, else the memory limit times
// the reads the query budget admits plus one; a listed system takes its
// own, else the top-level value, else derives it when its store opens.
func TestDuckDBMaxTempSize(t *testing.T) {
	mem := int64(300 << 20)
	for _, c := range []struct {
		budget, threads int
		want            int64
	}{
		{0, 2, 2 * mem},   // the shipped pods: 2 threads, budget 1
		{0, 12, 5 * mem},  // budget 4
		{2, 12, 3 * mem},  // query_budget 2
		{-1, 12, 6 * mem}, // budget off: 12/3 + 1 reads
		{-1, 2, 2 * mem},  // budget off: 2/3 + 1 reads
	} {
		cfg := Config{DuckDBMemory: ByteSize(mem), QueryBudget: c.budget}
		if got := cfg.DuckDBMaxTempSizeFor(c.threads); got != c.want {
			t.Errorf("budget %d, %d threads: %d, want %d", c.budget, c.threads, got, c.want)
		}
	}
	if got := (Config{DuckDBMemory: 300 << 20, DuckDBMaxTemp: 1 << 30}).DuckDBMaxTempSizeFor(12); got != 1<<30 {
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
`)
	want := map[string]int64{"a": 3 << 30, "b": 0}
	for _, s := range cfg.ResolvedSystems() {
		if s.DuckDBMaxTempSize != want[s.ID] {
			t.Errorf("%s: %d, want %d", s.ID, s.DuckDBMaxTempSize, want[s.ID])
		}
	}
	if got := cfg.ForSystem(cfg.ResolvedSystems()[1]).DuckDBMaxTempSizeFor(2); got != 512<<20 {
		t.Errorf("b derived: %d, want twice its 256 MiB", got)
	}
	cfg.DuckDBMaxTemp = 5 << 30
	want = map[string]int64{"a": 3 << 30, "b": 5 << 30}
	for _, s := range cfg.ResolvedSystems() {
		if s.DuckDBMaxTempSize != want[s.ID] {
			t.Errorf("top-level set, %s: %d, want %d", s.ID, s.DuckDBMaxTempSize, want[s.ID])
		}
	}
}
