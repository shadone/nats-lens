package checks

import (
	"strconv"
	"strings"
	"time"
)

// DurationText writes d the way ns_text does: 7d, 2h, 30s.
func DurationText(d time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case d%(24*time.Hour) == 0:
		return strconv.FormatInt(int64(d/(24*time.Hour)), 10) + "d"
	case d%time.Hour == 0:
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	case d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	case d%time.Second == 0:
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	return d.String()
}

// listLiteral writes names as a DuckDB VARCHAR[] literal.
func listLiteral(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "'" + strings.ReplaceAll(n, "'", "''") + "'"
	}
	return "[" + strings.Join(q, ", ") + "]::VARCHAR[]"
}
