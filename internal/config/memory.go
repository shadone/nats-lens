package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ByteSize is a byte count that YAML gives as a whole number with an
// optional KiB, MiB, GiB or TiB suffix.
type ByteSize int64

// UnmarshalText reads "512MiB", "2GiB" or "1073741824".
func (b *ByteSize) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}} {
		if v, ok := strings.CutSuffix(s, u.suffix); ok {
			s, mult = strings.TrimSpace(v), u.mult
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/mult {
		return fmt.Errorf("%q is not a byte size (a whole number with an optional KiB, MiB, GiB or TiB suffix)", string(text))
	}
	*b = ByteSize(n * mult)
	return nil
}

// cgroupMemoryFiles hold the container's memory limit, cgroup v2 first; a
// variable so that tests can point it elsewhere.
var cgroupMemoryFiles = []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}

// defaultMemoryLimit is DuckDB's limit when no cgroup limit is known.
const defaultMemoryLimit = 1 << 30

// TempPerMemory is the default bound of a database's spill, the
// temporary files DuckDB writes beside it when its queries need more than
// its memory limit, as a multiple of that limit. Proposed, not yet
// confirmed: under natslens-perf mixed with 2 threads and a 1 GiB limit
// the spill peaked at 1.07 times the limit
// (docs/operations/performance-baseline.md).
const TempPerMemory = 2

// DuckDBMaxTempSize returns the disk DuckDB may spill to:
// duckdb_max_temp_size when set, otherwise TempPerMemory times
// DuckDBMemoryLimit. DuckDB's own default is 90% of the free space of the
// disk, which beside the database is the data volume.
func (c Config) DuckDBMaxTempSize() int64 {
	if c.DuckDBMaxTemp > 0 {
		return int64(c.DuckDBMaxTemp)
	}
	return TempPerMemory * c.DuckDBMemoryLimit()
}

// DuckDBMemoryLimit returns the memory DuckDB may use: duckdb_memory_limit
// when set, otherwise 60% of the container's cgroup memory limit, otherwise
// 1 GiB. DuckDB's own default is 80% of the machine's memory, which in a
// container is the node's, not the pod's.
func (c Config) DuckDBMemoryLimit() int64 {
	if c.DuckDBMemory > 0 {
		return int64(c.DuckDBMemory)
	}
	for _, f := range cgroupMemoryFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		// "max" (v2) or a huge number (v1) means no limit.
		if err != nil || n <= 0 || n >= 1<<60 {
			return defaultMemoryLimit
		}
		return n * 6 / 10
	}
	return defaultMemoryLimit
}
