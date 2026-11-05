package checks

import "testing"

// A version threshold is one to three dot-separated numbers, given to the
// query as text and shown as given.
func TestParseVersionValue(t *testing.T) {
	for _, in := range []string{"1.37.0", "2.28", "0", " 3.9.0 "} {
		v, err := parseValue(UnitVersion, in)
		if err != nil {
			t.Errorf("parseValue(version, %q): %v", in, err)
			continue
		}
		if v.String() != v.Version || v.literal() != "'"+v.Version+"'" {
			t.Errorf("%q: text %q, literal %q", in, v.String(), v.literal())
		}
	}
	for _, in := range []string{"", "v1.2.3", "1.2.3.4", "1.2.3-beta", "1..2", "1.2'", "latest", "1.2.3; DROP"} {
		if _, err := parseValue(UnitVersion, in); err == nil {
			t.Errorf("parseValue(version, %q) accepted", in)
		}
	}
}
