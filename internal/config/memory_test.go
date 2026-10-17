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
