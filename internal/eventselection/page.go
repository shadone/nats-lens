// Package eventselection owns transport-only identity and page boundaries shared
// by the API and investigation client; it contains no database or UI policy.
package eventselection

import (
	"fmt"
	"strconv"
)

const MaxIDs = 2000
const MaxPageSize = 50

type Page struct {
	IDs                       []string
	Offset, Size, Next, Total int32
}

// ValidateIDs applies the same canonical identities before any native binding.
// ASVS 2.2.1/2.2.2: bounded positive identities; no input is interpolated into SQL.
func ValidateIDs(ids []string, boundary *int64, limit int) ([]int64, error) {
	if boundary != nil && *boundary < 0 {
		return nil, fmt.Errorf("event membership boundary must not be negative")
	}
	if len(ids) > limit {
		return nil, fmt.Errorf("at most %d selected event IDs are allowed", limit)
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || seen[n] {
			return nil, fmt.Errorf("selected event IDs must be unique positive decimal int64 values")
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}
func PageFor(ids []string, boundary *int64, offset, size int32) (Page, error) {
	if len(ids) == 0 {
		return Page{}, fmt.Errorf("an explicit event selection is required")
	}
	if _, err := ValidateIDs(ids, boundary, MaxIDs); err != nil {
		return Page{}, err
	}
	if size == 0 {
		size = MaxPageSize
	}
	if size < 1 || size > MaxPageSize {
		return Page{}, fmt.Errorf("event selection page size must be between 1 and 50")
	}
	if offset < 0 || offset >= int32(len(ids)) {
		return Page{}, fmt.Errorf("event selection offset is outside the supplied membership")
	}
	end := min(offset+size, int32(len(ids)))
	next := end
	if end == int32(len(ids)) {
		next = 0
	}
	return Page{IDs: ids[offset:end], Offset: offset, Size: size, Next: next, Total: int32(len(ids))}, nil
}
