package fake

import (
	"slices"
	"testing"
)

// The demo marks differing limits by the store provider's rule: the
// plurality, even short of half the servers, is not marked; a tie marks
// nothing.
func TestDemoLimitsDifferPlurality(t *testing.T) {
	spec := func(name string, payload int64) serverSpec { return serverSpec{name: name, maxPayload: payload} }
	cases := []struct {
		servers []serverSpec
		marked  []string
	}{
		{[]serverSpec{spec("a", 2), spec("b", 2), spec("c", 1), spec("d", 3), spec("e", 4)}, []string{"c", "d", "e"}},
		{[]serverSpec{spec("a", 0), spec("b", 8*mib)}, nil},
	}
	for _, c := range cases {
		sc := &scenario{servers: c.servers}
		var marked []string
		for _, sp := range sc.servers {
			if len(sc.limitsDiffer(sp)) > 0 {
				marked = append(marked, sp.name)
			}
		}
		if !slices.Equal(marked, c.marked) {
			t.Errorf("marked %v, want %v", marked, c.marked)
		}
	}
}
