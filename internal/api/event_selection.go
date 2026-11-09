package api

import (
	"strconv"
)

// EventSelection validates explicit identities before they reach the native
// driver. Boundaries are navigation data, never authorization or a full snapshot.
func EventSelection(ids []string, boundary *int64) ([]int64, error) {
	if boundary != nil && *boundary < 0 {
		return nil, invalid("event membership boundary must not be negative")
	}
	if len(ids) > 1000 {
		return nil, invalid("at most 1000 selected event IDs are allowed")
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || seen[n] {
			return nil, invalid("selected event IDs must be unique positive decimal int64 values")
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}
