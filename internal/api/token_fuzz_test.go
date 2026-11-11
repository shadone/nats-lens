package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

// FuzzDecodeCursor reads offset and keyset page tokens: a token is refused
// as invalid, or holds bounded values and re-encodes to a token that reads
// the same; Page then returns a slice of its list.
func FuzzDecodeCursor(f *testing.F) {
	for _, c := range []Cursor{{}, {Offset: 1}, {Offset: MaxOffset}, {Time: time.Date(2026, 10, 17, 14, 30, 0, 123, time.UTC), ID: 991}, {Epoch: 7, Upto: 9}} {
		f.Add(EncodeCursor(c), uint8(10), int32(3))
	}
	for _, s := range []string{
		"", "!", "e30", token64(`{"o":-1}`), token64(`{"o":1048577}`), token64(`{"i":-1}`), token64(`{"o":1e3}`), token64(`{"o":"1"}`),
		token64(`{"t":"0001-01-01T00:00:00Z"}`), token64(`{"t":"9999-12-31T23:59:59.999999999+14:00"}`), token64(`{"t":"2026-10-17T14:30:00-23:59"}`),
		token64(`{"t":"10000-01-01T00:00:00Z"}`), token64(`{"o":1,"O":2}`), token64(`{"o":1} {"o":2}`), token64(`{"e":9223372036854775807}`),
		token64(`{"e":9223372036854775808}`), token64("{\"o\":1,\"x\":\"\xff\"}"), "eyJvIjoxfQ==", "eyJvIjoxfQ",
	} {
		f.Add(s, uint8(10), int32(3))
	}
	f.Fuzz(func(t *testing.T, token string, items uint8, size int32) {
		if len(token) > 4096 {
			return
		}
		c, err := DecodeCursor(token)
		if err != nil {
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(c, Cursor{}) {
				t.Fatalf("DecodeCursor(%q) = %+v, %v; want an invalid-argument error and no cursor", token, c, err)
			}
			return
		}
		if c.Offset < 0 || c.Offset > MaxOffset || c.ID < 0 || c.Epoch < 0 || c.Upto < 0 {
			t.Fatalf("DecodeCursor(%q) accepted %+v", token, c)
		}
		again := EncodeCursor(c)
		back, err := DecodeCursor(again)
		if err != nil || back.Offset != c.Offset || !back.Time.Equal(c.Time) || back.ID != c.ID || back.Epoch != c.Epoch || back.Upto != c.Upto {
			t.Fatalf("cursor %+v re-encodes as %q, which reads %+v, %v", c, again, back, err)
		}
		if EncodeCursor(back) != again {
			t.Fatalf("cursor %+v does not encode stably", c)
		}
		if size < 1 {
			return
		}
		list := make([]int, items)
		for i := range list {
			list[i] = i
		}
		page, next, err := Page(list, token, size)
		if err != nil {
			t.Fatalf("Page refused a token DecodeCursor accepts: %v", err)
		}
		if len(page) > int(size) || (len(page) > 0 && page[0] != c.Offset) {
			t.Fatalf("page of %d from %d by %d: %v", items, c.Offset, size, page)
		}
		if next != "" {
			n, err := DecodeCursor(next)
			if err != nil || n.Offset != c.Offset+len(page) || n.Offset >= len(list) {
				t.Fatalf("next token %+v after %d items from %d, %v", n, len(page), c.Offset, err)
			}
		}
	})
}

// FuzzEntityToken reads entity list page tokens: a token is refused as
// invalid, or holds an ID of 22 characters, an offset in range and both
// range bounds, encodes again to a token that reads the same, and pins the
// omitted bounds of a request to a valid range or an error.
func FuzzEntityToken(f *testing.F) {
	valid := `{"id":"AAAAAAAAAAAAAAAAAAAAAA","issuedAt":"1792247400","offset":100,"range":{"start":{"seconds":1792242000},"end":{"seconds":1792245600}}}`
	for _, s := range []string{
		valid,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":1,"range":{"start":{},"end":{}}}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":1,"range":{"start":{"seconds":-62135596801},"end":{"seconds":253402300800}}}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":1,"range":{"start":{"seconds":1,"nanos":-1},"end":{"seconds":2,"nanos":1000000000}}}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":1,"range":{"start":{"seconds":9223372036854775807},"end":{"seconds":-9223372036854775808}}}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":0,"range":{"start":{},"end":{}}}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":1048577,"range":{"start":{},"end":{}}}`,
		`{"id":"<<<<<<<<<<<<<<<<<<<<<<","offset":1,"range":{"start":{},"end":{}}}`,
		`{"id":"ééééééééééé","offset":1,"range":{"start":{},"end":{}}}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":1,"range":{"start":{}}}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","offset":1,"range":null}`,
		`{"id":"AAAAAAAAAAAAAAAAAAAAAA","issuedAt":"x","offset":1,"range":{"start":{},"end":{}}}`,
	} {
		f.Add(token64(s))
	}
	f.Add(strings.Repeat("A", 1025))
	now := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > 4096 {
			return
		}
		tok, err := readEntityToken(token)
		if err != nil {
			if !errors.Is(err, ErrInvalid) || tok.ID != "" || tok.Range != nil {
				t.Fatalf("readEntityToken(%q) = %+v, %v; want an invalid-argument error and no token", token, tok, err)
			}
			if _, err := EntityListRange(nil, token, now); token != "" && !errors.Is(err, ErrInvalid) {
				t.Fatalf("EntityListRange accepted a token readEntityToken refuses: %v", err)
			}
			return
		}
		if len(tok.ID) != 22 || tok.Offset < 1 || tok.Offset > MaxOffset || tok.Range.GetStart() == nil || tok.Range.GetEnd() == nil {
			t.Fatalf("readEntityToken(%q) accepted %+v", token, tok)
		}
		b, err := json.Marshal(tok)
		if err != nil {
			t.Fatal(err)
		}
		again := base64.RawURLEncoding.EncodeToString(b)
		if len(again) <= 1024 {
			back, err := readEntityToken(again)
			if err != nil || back.ID != tok.ID || back.IssuedAt != tok.IssuedAt || back.Offset != tok.Offset || !proto.Equal(back.Range, tok.Range) {
				t.Fatalf("token %s re-encodes as %s, which reads %+v, %v", b, again, back, err)
			}
		}
		r, err := EntityListRange(nil, token, now)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("EntityListRange(%s) = %v; want an invalid-argument error", b, err)
			}
			return
		}
		checkResolvedRange(t, r.GetStart().AsTime(), r.GetEnd().AsTime())
		if !r.GetStart().AsTime().Equal(tok.Range.GetStart().AsTime()) || !r.GetEnd().AsTime().Equal(tok.Range.GetEnd().AsTime()) {
			t.Fatalf("EntityListRange(%s) = %v, not the token's range", b, r)
		}
	})
}

// FuzzEpisodeCursor reads occurrence page tokens: a token is refused, as
// invalid or as expired, or its next-page token reads the same cursor.
func FuzzEpisodeCursor(f *testing.F) {
	issued := time.Now().Unix()
	scope := strings.Repeat("ab", 32)
	from := time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC)
	c := EpisodeCursor{Membership: "3:12345", Offset: 50, ReadEpoch: 9007199254740993, Total: 120, From: from, To: from.Add(time.Hour), Issued: issued, Scope: scope}
	f.Add(c.Next(c.ReadEpoch, c.Total, c.Offset))
	f.Add(c.Next(1, 1, 1))
	f.Add(c.Next(1, 1, 2))
	f.Add(c.Next(0, 1, 0))
	f.Add(c.Next(1, 2147483647, MaxOffset+1))
	for _, s := range []string{
		"", "fep1.", "fep2.e30", "fep1.!", "fep1." + strings.Repeat("A", 2048),
		"fep1." + token64(`{"m":"3:12345","o":1,"e":"1","n":2,"f":"2026-10-17T13:00:00+14:00","t":"2026-10-17T13:00:00-12:00","a":"`+strconv.FormatInt(issued, 10)+`","s":"`+scope+`"}`),
		"fep1." + token64(`{"m":"3:12345","o":1,"e":"1","n":2,"f":"2026-10-17T13:00:00Z","t":"2026-10-17T14:00:00Z","a":"1","s":"`+scope+`"}`),
		"fep1." + token64(`{"m":"3:12345","o":1,"e":"1","n":2,"f":"2026-10-17T13:00:00Z","t":"2026-10-17T14:00:00Z","a":"9223372036854775807","s":"`+scope+`"}`),
		"fep1." + token64(`{"m":"3:12345","o":1,"e":1,"n":2,"f":"2026-10-17T13:00:00Z","t":"2026-10-17T14:00:00Z","a":"1","s":"`+scope+`"}`),
		"fep1." + token64(`{"m":"3:12345","o":1,"e":"1","n":2,"f":"2026-10-17T13:00:00Z","t":"2026-10-17T14:00:00Z","a":"1","s":"`+scope+`","x":1}`),
		"fep1." + token64(`{"m":"::","o":1,"e":"1","n":2,"f":"2026-10-17T13:00:00Z","t":"2026-10-17T14:00:00Z","a":"1","s":"`+scope+`"}`),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > 4096 {
			return
		}
		got, err := decodeEpisodeCursor(token)
		if err != nil {
			if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrFailedPrecondition) {
				t.Fatalf("decodeEpisodeCursor(%q) = %v; want an invalid or expired error", token, err)
			}
			return
		}
		if got.Offset < 0 || got.Offset > int(got.Total) || got.ReadEpoch < 1 || !got.From.Before(got.To) || len(got.Scope) != 64 {
			t.Fatalf("decodeEpisodeCursor(%q) accepted %+v", token, got)
		}
		next := got.Next(got.ReadEpoch, got.Total, got.Offset)
		back, err := decodeEpisodeCursor(next)
		if errors.Is(err, ErrFailedPrecondition) {
			return // expired between the two reads
		}
		if err != nil || back.Membership != got.Membership || back.Offset != got.Offset || back.ReadEpoch != got.ReadEpoch ||
			back.Total != got.Total || !back.From.Equal(got.From) || !back.To.Equal(got.To) || back.Issued != got.Issued || back.Scope != got.Scope {
			t.Fatalf("cursor %+v re-encodes as %q, which reads %+v, %v", got, next, back, err)
		}
	})
}
