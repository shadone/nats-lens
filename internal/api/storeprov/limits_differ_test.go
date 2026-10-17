package storeprov

import (
	"database/sql"
	"slices"
	"testing"
)

func TestLimitsDiffer(t *testing.T) {
	// payload builds a server's limits with only max_payload set; 0 leaves
	// every limit unreported, as without VARZ and JSZ.
	payload := func(v int64) serverLimits {
		return serverLimits{payload: sql.NullInt64{Int64: v, Valid: v != 0}}
	}
	cases := []struct {
		name    string
		servers map[string]serverLimits
		marked  []string // servers with max_payload marked, sorted
	}{
		{"two that disagree", map[string]serverLimits{"a": payload(1), "b": payload(2)}, nil},
		{"a 2/2 split", map[string]serverLimits{"a": payload(1), "b": payload(1), "c": payload(2), "d": payload(2)}, nil},
		{"a 2/1/1 plurality", map[string]serverLimits{"a": payload(1), "b": payload(1), "c": payload(2), "d": payload(3)}, []string{"c", "d"}},
		{"unreported is not counted", map[string]serverLimits{"a": payload(1), "b": payload(0), "c": payload(0), "d": payload(2)}, nil},
		{"unreported is not marked", map[string]serverLimits{"a": payload(1), "b": payload(1), "c": payload(0), "d": payload(2)}, []string{"d"}},
		{"VARZ missing on one of three", map[string]serverLimits{"a": payload(1), "b": payload(8), "c": {}}, nil},
		{"agreeing two and one without VARZ", map[string]serverLimits{"a": payload(1), "b": payload(1), "c": {}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var marked []string
			for name := range c.servers {
				d := limitsDiffer(c.servers, name)
				if len(d) > 0 {
					if !slices.Equal(d, []string{"max_payload"}) {
						t.Errorf("%s marked %v", name, d)
					}
					marked = append(marked, name)
				}
			}
			slices.Sort(marked)
			if !slices.Equal(marked, c.marked) {
				t.Errorf("marked %v, want %v", marked, c.marked)
			}
		})
	}
}
