package eventselection

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Link is untrusted navigation data, never a source destination or authorization.
// ASVS 1.5.1/2.2.2: exact keys, bounded decoding, canonical IDs and bound scope.
type Link struct {
	Version     int      `json:"version"`
	System      string   `json:"system"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	IDs         []string `json:"ids"`
	EventReadID *string  `json:"eventReadId,omitempty"`
	Limits      []string `json:"limits"`
}

func FromURL(u *url.URL) (Link, Page, error) {
	var s Link
	bad := func() (Link, Page, error) {
		return Link{}, Page{}, fmt.Errorf("invalid captured event selection link or changed system/range")
	}
	if u.Path != "/timeline/events" || len(u.Fragment) > 88000 || !strings.HasPrefix(u.Fragment, "selection=") {
		return bad()
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(u.Fragment, "selection="))
	if err != nil || len(raw) > 65536 || !utf8.Valid(raw) {
		return bad()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return bad()
	}
	for k := range fields {
		switch k {
		case "version", "system", "from", "to", "ids", "eventReadId", "limits":
		default:
			return bad()
		}
	}
	for _, k := range []string{"version", "system", "from", "to", "ids", "limits"} {
		if len(fields[k]) == 0 || string(fields[k]) == "null" {
			return bad()
		}
	}
	if v, ok := fields["eventReadId"]; ok && string(v) == "null" {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF {
		return bad()
	}
	if s.Version != 1 || s.System == "" || len(s.System) > 256 || strings.ContainsFunc(s.System, func(r rune) bool { return r < 32 || r == 127 }) {
		return bad()
	}
	a, ae := time.Parse(time.RFC3339Nano, s.From)
	b, be := time.Parse(time.RFC3339Nano, s.To)
	if ae != nil || be != nil || !a.Before(b) || !strings.HasSuffix(s.From, "Z") || !strings.HasSuffix(s.To, "Z") || b.Sub(a) > 30*24*time.Hour {
		return bad()
	}
	var boundary *int64
	if s.EventReadID != nil {
		n, e := strconv.ParseInt(*s.EventReadID, 10, 64)
		if e != nil || n < 0 || strconv.FormatInt(n, 10) != *s.EventReadID {
			return bad()
		}
		boundary = &n
	}
	seen := map[string]bool{}
	for _, v := range s.Limits {
		if seen[v] {
			return bad()
		}
		switch v {
		case "NATS and collector events", "finding transitions", "Kubernetes events", "server lane items", "leader elections":
		default:
			return bad()
		}
		seen[v] = true
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return bad()
	}
	for k, values := range q {
		if len(values) != 1 {
			return bad()
		}
		switch k {
		case "system", "from", "to", "eventReadId", "offset", "pageSize":
		default:
			return bad()
		}
	}
	if q.Get("system") != s.System || q.Get("from") != s.From || q.Get("to") != s.To {
		return bad()
	}
	if (s.EventReadID == nil && q.Has("eventReadId")) || (s.EventReadID != nil && q.Get("eventReadId") != *s.EventReadID) {
		return bad()
	}
	number := func(key string, fallback int32) (int32, error) {
		if !q.Has(key) {
			return fallback, nil
		}
		v, e := strconv.ParseInt(q.Get(key), 10, 32)
		if e != nil || strconv.FormatInt(v, 10) != q.Get(key) {
			return 0, fmt.Errorf("invalid page")
		}
		return int32(v), nil
	}
	offset, err := number("offset", 0)
	if err != nil {
		return bad()
	}
	size, err := number("pageSize", 50)
	if err != nil || size < 1 {
		return bad()
	}
	page, err := PageFor(s.IDs, boundary, offset, size)
	if err != nil {
		return bad()
	}
	return s, page, nil
}
