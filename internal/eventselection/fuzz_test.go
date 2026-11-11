package eventselection

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const fuzzQuery = "eventReadId=3&from=2026-10-17T12%3A00%3A00Z&system=shop&to=2026-10-17T13%3A00%3A00Z"

// FuzzFromURL reads captured event selection links: a link names one page
// of its own membership, and a link it accepts, written again from what it
// read, reads back the same. The selection is fuzzed as JSON, before its
// base64 encoding, so that the fuzzer reaches the field checks.
func FuzzFromURL(f *testing.F) {
	valid := `{"version":1,"system":"shop","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["1","2"],"eventReadId":"3","limits":[]}`
	f.Add(fuzzQuery, []byte(valid))
	f.Add(fuzzQuery+"&offset=1&pageSize=1", []byte(valid))
	f.Add(fuzzQuery+"&offset=2", []byte(valid))
	f.Add(fuzzQuery+"&offset=01", []byte(valid))
	f.Add(fuzzQuery+"&pageSize=2147483648", []byte(valid))
	f.Add(fuzzQuery+"&system=shop", []byte(valid))
	f.Add(fuzzQuery+"&x=%zz", []byte(valid))
	f.Add("from=2026-10-17T12%3A00%3A00Z&system=shop&to=2026-10-17T13%3A00%3A00Z",
		[]byte(`{"version":1,"system":"shop","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["9223372036854775807"],"limits":["leader elections","Kubernetes events"]}`))
	f.Add(fuzzQuery, []byte(`{"version":1,"system":"shop","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["1","1"],"eventReadId":"3","limits":[]}`))
	f.Add(fuzzQuery, []byte(`{"version":1,"system":"shop","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["01"],"eventReadId":"3","limits":[]}`))
	f.Add(fuzzQuery, []byte(`{"version":1,"system":"shop","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["1"],"eventReadId":null,"limits":[]}`))
	f.Add(fuzzQuery, []byte(`{"version":1,"system":"shop","from":"2026-10-17T12:00:00+00:00","to":"2026-10-17T13:00:00Z","ids":["1"],"eventReadId":"3","limits":[]}`))
	f.Add(fuzzQuery, []byte(`{"version":1,"system":"shop","from":"0000-01-01T00:00:00Z","to":"9999-12-31T23:59:59.999999999Z","ids":["1"],"eventReadId":"3","limits":[]}`))
	f.Add(fuzzQuery, []byte(`{"version":1,"System":"shop","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["1"],"eventReadId":"3","limits":[]}`))
	f.Add(fuzzQuery, []byte(`{"version":1,"system":"sh\u0000op","from":"2026-10-17T12:00:00Z","to":"2026-10-17T13:00:00Z","ids":["1"],"eventReadId":"3","limits":[]}`))
	f.Add("", []byte(`[]`))
	f.Add("", []byte{0xff})
	f.Fuzz(func(t *testing.T, query string, selection []byte) {
		if len(query) > 4096 || len(selection) > 16<<10 {
			return
		}
		u := &url.URL{Path: "/timeline/events", RawQuery: query, Fragment: "selection=" + base64.RawURLEncoding.EncodeToString(selection)}
		s, p, err := FromURL(u)
		s2, p2, err2 := FromURL(u)
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(s, s2) || !reflect.DeepEqual(p, p2) {
			t.Fatalf("FromURL is not deterministic for %q", selection)
		}
		if err != nil {
			if !reflect.DeepEqual(s, Link{}) || !reflect.DeepEqual(p, Page{}) {
				t.Fatalf("refused link returned %+v %+v", s, p)
			}
			return
		}
		n := int32(len(p.IDs))
		if s.Version != 1 || p.Total != int32(len(s.IDs)) || n < 1 || n > p.Size || p.Size > MaxPageSize {
			t.Fatalf("page %+v does not fit a membership of %d", p, len(s.IDs))
		}
		if !slices.Equal(p.IDs, s.IDs[p.Offset:p.Offset+n]) {
			t.Fatalf("page %+v is not the membership's slice at its offset", p)
		}
		if end := p.Offset + n; (end == p.Total) != (p.Next == 0) || (p.Next != 0 && p.Next != end) {
			t.Fatalf("page %+v has next %d", p, p.Next)
		}
		again, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		u.Fragment = "selection=" + base64.RawURLEncoding.EncodeToString(again)
		rs, rp, err := FromURL(u)
		if err != nil || !reflect.DeepEqual(rs, s) || !reflect.DeepEqual(rp, p) {
			t.Fatalf("link %s written again as %s reads %+v %+v, %v", selection, again, rs, rp, err)
		}
	})
}

// FuzzPageFor pages through a selection: paging from the first page by
// Next visits every ID once, in order, and an invalid selection or page is
// an error.
func FuzzPageFor(f *testing.F) {
	f.Add("1,2,3", int64(-1), int32(0), int32(0))
	f.Add("1,2,3", int64(3), int32(1), int32(1))
	f.Add("3,2,1,4,5", int64(0), int32(4), int32(2))
	f.Add("1,1", int64(-1), int32(0), int32(0))
	f.Add("0", int64(-1), int32(0), int32(0))
	f.Add("+1", int64(-1), int32(0), int32(0))
	f.Add("9223372036854775807,9223372036854775808", int64(-1), int32(0), int32(0))
	f.Add("", int64(-1), int32(0), int32(0))
	f.Add("1,2", int64(-1), int32(2), int32(50))
	f.Add("1,2", int64(-1), int32(-1), int32(51))
	f.Add("1,2", int64(-2), int32(0), int32(2147483647))
	f.Add(strings.Repeat("7,", 30)+"8", int64(-1), int32(0), int32(0))
	f.Fuzz(func(t *testing.T, list string, boundary int64, offset, size int32) {
		if len(list) > 32<<10 {
			return
		}
		var ids []string
		if list != "" {
			ids = strings.Split(list, ",")
		}
		var b *int64
		if boundary != -1 {
			b = &boundary
		}
		p, err := PageFor(ids, b, offset, size)
		if err != nil {
			if !reflect.DeepEqual(p, Page{}) {
				t.Fatalf("refused page returned %+v", p)
			}
			return
		}
		if _, err := ValidateIDs(ids, b, MaxIDs); err != nil {
			t.Fatalf("PageFor accepted IDs that ValidateIDs refuses: %v", err)
		}
		var seen []string
		next := int32(0)
		for range len(ids) + 1 {
			q, err := PageFor(ids, b, next, size)
			if err != nil {
				t.Fatalf("page at %d of %d: %v", next, len(ids), err)
			}
			seen = append(seen, q.IDs...)
			if q.Next == 0 {
				break
			}
			if q.Next <= next {
				t.Fatalf("page at %d points back to %d", next, q.Next)
			}
			next = q.Next
		}
		if !slices.Equal(seen, ids) {
			t.Fatalf("paging %d IDs by %d visited %d", len(ids), size, len(seen))
		}
	})
}
