package api

import (
	"fmt"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// LastPoint is the series' newest value; nil without points. A tile shows
// it so that its number and its chart use the same reduction.
func LastPoint(s *v1.Series) *float64 {
	if pts := s.GetPoints(); len(pts) > 0 {
		v := pts[len(pts)-1].GetValue()
		return &v
	}
	return nil
}

// SumPoints is the total of the series' values; nil without points.
func SumPoints(s *v1.Series) *float64 {
	if len(s.GetPoints()) == 0 {
		return nil
	}
	var t float64
	for _, p := range s.GetPoints() {
		t += p.GetValue()
	}
	return &t
}

// Plural writes a count with its noun, for example "1 drain" or
// "3 drains".
func Plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// StreamsSubtitle is the streams tile's line, for example
// "7 of 9 replicated · 1 lagging"; empty while the count is unset.
func StreamsSubtitle(value *float64, replicated, total, lagging int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d of %d replicated · %d lagging", replicated, total, lagging)
}

// KVSubtitle is the KV tile's line, for example "1 R1 · 0 lagging"; empty
// while the count is unset.
func KVSubtitle(value *float64, single, lagging int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d R1 · %d lagging", single, lagging)
}

// ObjectStoresSubtitle is the object stores tile's line; empty while the
// count is unset.
func ObjectStoresSubtitle(value *float64, lagging int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d lagging", lagging)
}

// ConsumersSubtitle counts the consumers behind, the firing consumer-002
// findings; empty while the count is unset or that check did not run.
func ConsumersSubtitle(value *float64, ran bool, behind int) string {
	if value == nil || !ran {
		return ""
	}
	return fmt.Sprintf("%d behind", behind)
}

// ClientsSubtitle names the applications and the slow consumer events in
// the range, for example "8 apps · 7 slow". Connections the servers count
// that no CONNZ entry names give "no named connections", never "0 apps".
func ClientsSubtitle(value *float64, apps int, slow *float64) string {
	if value == nil {
		return ""
	}
	parts := []string{"no named connections"}
	if apps > 0 {
		parts[0] = Plural(apps, "app")
	}
	if slow != nil {
		parts = append(parts, fmt.Sprintf("%.0f slow", *slow))
	}
	return strings.Join(parts, " · ")
}

// StorageSubtitle says that the value counts every replica, then the share
// of the limit used and the growth per day, each when known.
func StorageSubtitle(used, limit, perDay *float64) string {
	parts := []string{"all replicas"}
	if used != nil && limit != nil && *limit > 0 {
		parts = append(parts, fmt.Sprintf("%.1f%% used", *used / *limit * 100))
	}
	if perDay != nil {
		parts = append(parts, FmtGrowth(*perDay))
	}
	return strings.Join(parts, " · ")
}
