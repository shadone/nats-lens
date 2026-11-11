package eventselection

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func linkFixture(t *testing.T) *url.URL {
	t.Helper()
	data := `{"version":1,"system":"shop","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["1","2"],"eventReadId":"3","limits":[]}`
	q := url.Values{"system": {"shop"}, "from": {"2026-10-17T12:00:00Z"}, "to": {"2026-10-17T13:00:00Z"}, "eventReadId": {"3"}}
	return &url.URL{Path: "/timeline/events", RawQuery: q.Encode(), Fragment: "selection=" + base64.RawURLEncoding.EncodeToString([]byte(data))}
}
func TestCapturedLinkValidation(t *testing.T) {
	for _, change := range []func(*url.URL){
		func(u *url.URL) { u.Path = "/events" }, func(u *url.URL) { u.Fragment = "" }, func(u *url.URL) { u.Fragment = "selection=!" }, func(u *url.URL) { u.Fragment = "selection=" + strings.Repeat("a", 88000) }, func(u *url.URL) { u.Fragment = "selection=" + base64.RawURLEncoding.EncodeToString([]byte{255}) },
		func(u *url.URL) { u.RawQuery += "&system=shop" }, func(u *url.URL) { u.RawQuery += "&unknown=1" }, func(u *url.URL) { u.RawQuery += "&offset=-1" }, func(u *url.URL) { u.RawQuery += "&offset=2" }, func(u *url.URL) { u.RawQuery += "&offset=01" }, func(u *url.URL) { u.RawQuery += "&pageSize=0" }, func(u *url.URL) { u.RawQuery += "&pageSize=51" }, func(u *url.URL) { u.RawQuery += "&pageSize=2147483648" },
	} {
		u := linkFixture(t)
		change(u)
		if _, _, err := FromURL(u); err == nil {
			t.Fatal("invalid captured link accepted")
		}
	}
	for name, value := range map[string]any{"version": 2, "system": "", "from": "bad", "to": "2026-10-17T11:00:00Z", "ids": []string{"1", "1"}, "eventReadId": "-1", "limits": []string{"unknown"}, "SYSTEM": "shop"} {
		t.Run(name, func(t *testing.T) {
			u := linkFixture(t)
			raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Fragment, "selection="))
			var fields map[string]any
			_ = json.Unmarshal(raw, &fields)
			fields[name] = value
			data, _ := json.Marshal(fields)
			u.Fragment = "selection=" + base64.RawURLEncoding.EncodeToString(data)
			if _, _, err := FromURL(u); err == nil {
				t.Fatal("invalid field accepted")
			}
		})
	}
	for _, key := range []string{"version", "system", "from", "to", "ids", "limits"} {
		u := linkFixture(t)
		raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Fragment, "selection="))
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		delete(fields, key)
		data, _ := json.Marshal(fields)
		u.Fragment = "selection=" + base64.RawURLEncoding.EncodeToString(data)
		if _, _, err := FromURL(u); err == nil {
			t.Fatalf("missing %s accepted", key)
		}
	}
	u := linkFixture(t)
	s, p, err := FromURL(u)
	if err != nil || p.Total != 2 || p.Size != 50 || len(s.IDs) != 2 {
		t.Fatalf("valid link: %+v %v", p, err)
	}
}

// Every lane's limit label a Timeline selection can carry is accepted.
func TestCapturedLinkLaneLimits(t *testing.T) {
	u := linkFixture(t)
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Fragment, "selection="))
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	fields["limits"] = []string{"NATS and collector events", "finding transitions", "Kubernetes events", "server lane items", "leader elections"}
	data, _ := json.Marshal(fields)
	u.Fragment = "selection=" + base64.RawURLEncoding.EncodeToString(data)
	if _, _, err := FromURL(u); err != nil {
		t.Fatal(err)
	}
}
