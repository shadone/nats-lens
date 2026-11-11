package api

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A page token's time must be one a next token can carry: encoding/json
// writes no zone offset of 24 hours or more, and a time with one would
// make EncodeCursor return "", the first page's token, and Next a token
// that does not read.
func TestPageTokenTimeMustEncode(t *testing.T) {
	for _, at := range []string{"3044-10-17T13:43:40-24:48", "2026-10-17T13:00:00+24:00"} {
		if c, err := DecodeCursor(token64(`{"t":"` + at + `"}`)); !errors.Is(err, ErrInvalid) {
			t.Errorf("DecodeCursor accepted time %s: %+v, %v", at, c, err)
		}
		scope := strings.Repeat("ab", 32)
		issued := strconv.FormatInt(time.Now().Unix(), 10)
		for _, body := range []string{
			`{"m":"3:12345","o":1,"e":"1","n":2,"f":"` + at + `","t":"9999-12-31T00:00:00Z","a":"` + issued + `","s":"` + scope + `"}`,
			`{"m":"3:12345","o":1,"e":"1","n":2,"f":"0001-01-02T00:00:00Z","t":"` + at + `","a":"` + issued + `","s":"` + scope + `"}`,
		} {
			if c, err := decodeEpisodeCursor("fep1." + token64(body)); !errors.Is(err, ErrInvalid) {
				t.Errorf("decodeEpisodeCursor accepted %s: next token %q, %v", body, c.Next(c.ReadEpoch, c.Total, c.Offset), err)
			}
		}
	}
}

func token64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
