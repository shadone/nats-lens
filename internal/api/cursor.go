package api

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// MaxOffset is the largest offset a page token may hold.
const MaxOffset = 1 << 20

// Cursor is the content of a page token. Offset paging serves lists that
// are built whole (findings, mutes, connections); keyset paging serves
// events, which arrive while a reader pages through them.
type Cursor struct {
	// Offset is the index of the first item of the next page.
	Offset int `json:"o,omitempty"`
	// Time and ID are the last item of the previous page; the next page
	// starts with the item that sorts after them (older, newest first).
	Time time.Time `json:"t,omitzero"`
	ID   int64     `json:"i,omitempty"`
	// Epoch pins a list of one epoch's rows to the epoch of its first page.
	Epoch int64 `json:"e,omitempty"`
}

// EncodeCursor returns the page token for c.
func EncodeCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor reads a page token; an empty token is the first page.
func DecodeCursor(token string) (Cursor, error) {
	var c Cursor
	if token == "" {
		return c, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return c, invalid("page_token is not valid")
	}
	if err := json.Unmarshal(b, &c); err != nil || c.Offset < 0 || c.Offset > MaxOffset || c.ID < 0 || c.Epoch < 0 {
		return Cursor{}, invalid("page_token is not valid")
	}
	return c, nil
}

// Page returns one page of items by offset, with the token of the next
// page or "" on the last one.
func Page[T any](items []T, token string, size int32) ([]T, string, error) {
	c, err := DecodeCursor(token)
	if err != nil {
		return nil, "", err
	}
	if c.Offset >= len(items) {
		return nil, "", nil
	}
	end := min(c.Offset+int(size), len(items))
	next := ""
	if end < len(items) {
		next = EncodeCursor(Cursor{Offset: end})
	}
	return items[c.Offset:end], next, nil
}
