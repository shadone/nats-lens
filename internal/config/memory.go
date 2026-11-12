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

// SpillReads is how many reads the default spill bound allows for: the
// query budget of a database with threads, or threads/3 + 1 with the
// budget off. Under natslens-perf mixed the spill grew with the reads the
// budget admits: 1.07 times the memory limit at a budget of 1, about 3
// times at 4 (docs/operations/performance-baseline.md).
func (c Config) SpillReads(threads int) int64 {
	if b := c.QueryBudgetFor(threads); b > 0 {
		return int64(b)
	}
	return int64(threads/ThreadsPerQuery + 1)
}

// DuckDBMaxTempSizeFor returns the disk the DuckDB of a database with
// threads may spill to: duckdb_max_temp_size when set, otherwise
// (SpillReads + 1) times DuckDBMemoryLimit, twice the limit at the budget
// of 1 that 2 threads get. The caller lowers the default to 90% of the
// free disk, DuckDB's own bound. Proposed, not yet confirmed.
func (c Config) DuckDBMaxTempSizeFor(threads int) int64 {
	if c.DuckDBMaxTemp > 0 {
		return int64(c.DuckDBMaxTemp)
	}
	return (c.SpillReads(threads) + 1) * c.DuckDBMemoryLimit()
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
