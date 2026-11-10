package checks

import (
	"strings"
	"testing"
)

func acceptHeader(entity, extra string) []byte {
	return []byte("/*\ncode: " + entity + "-900\nname: n\nentity: " + entity + "\ncategory: storage\nseverity: info\nkind: operational\n" +
		extra + "description: d\nremediation: r\n*/\nSELECT 1")
}

func TestAcceptanceHeaderRules(t *testing.T) {
	for _, c := range []struct{ entity, extra, err string }{
		{"stream", "acceptable: true\naccept_review: growth\n", ""},
		{"consumer", "acceptable: true\n", ""},
		{"stream", "accept_review: growth\n", "accept_review needs acceptable"},
		{"consumer", "acceptable: true\naccept_review: growth\n", "stream or server"},
		{"stream", "acceptable: true\naccept_review: weekly\n", "is not growth"},
		{"system", "acceptable: true\n", "system check cannot be acceptable"},
	} {
		_, err := parse(c.entity+"-900.sql", acceptHeader(c.entity, c.extra))
		if (c.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s %q: %v, want %q", c.entity, c.extra, err, c.err)
		}
	}
}
