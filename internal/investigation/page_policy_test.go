package investigation

import (
	"encoding/json"
	"net/url"
	"os"
	"testing"
)

func TestConnectionsUIContract(t *testing.T) {
	b, err := os.ReadFile("testdata/connection_scope.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Tooling  string
		IncludeTooling bool
	}
	if err = json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		q := url.Values{"system": {"shop"}, "name": {tc.Name}}
		if tc.Tooling != "" {
			q.Set("tooling", tc.Tooling)
		}
		c, err := ContextFromURL("https://lens.example.com/connections?"+q.Encode(), "https://lens.example.com", fixtureNow())
		if err != nil {
			t.Fatal(err)
		}
		original, _ := json.Marshal(c)
		for _, override := range []string{"", `{"includeTooling":false}`, `{"includeTooling":true}`} {
			req, _, err := Prepare("ListConnections", c, []byte(override))
			if err != nil {
				t.Fatal(err)
			}
			var r struct {
				IncludeTooling bool
				NameContains   string
			}
			json.Unmarshal(req, &r)
			want := tc.IncludeTooling
			if override != "" {
				want = override == `{"includeTooling":true}`
			}
			if r.IncludeTooling != want || r.NameContains != tc.Name {
				t.Errorf("%+v override %s: %s", tc, override, req)
			}
			_, limitations, err := effectiveContext("ListConnections", c, req)
			if err != nil {
				t.Fatal(err)
			}
			if (len(limitations) > 0) != (tc.Name != "" && !want) {
				t.Errorf("wrong scope disclosure for %+v override %s: %v", tc, override, limitations)
			}
		}
		after, _ := json.Marshal(c)
		if string(original) != string(after) {
			t.Fatal("raw filters mutated")
		}
	}
}
