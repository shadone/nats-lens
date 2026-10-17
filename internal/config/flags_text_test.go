package config

import (
	"flag"
	"regexp"
	"testing"
)

// The flags' help says "collection", as the UI does; "epoch" stays in
// identifiers only.
func TestFlagHelpSaysCollection(t *testing.T) {
	var got *flag.FlagSet
	if _, err := ParseWith("natslens", nil, func(fs *flag.FlagSet) { got = fs }); err != nil {
		t.Fatal(err)
	}
	word := regexp.MustCompile(`\bepochs?\b`)
	got.VisitAll(func(f *flag.Flag) {
		if word.MatchString(f.Usage) {
			t.Errorf("-%s: %q", f.Name, f.Usage)
		}
	})
}
