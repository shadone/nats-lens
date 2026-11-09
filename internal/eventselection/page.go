// Package eventselection owns transport-only identity and page boundaries shared
// by the API and investigation client; it contains no database or UI policy.
package eventselection

const MaxIDs = 2000
const MaxPageSize = 50

type Page struct {
	IDs                       []string
	Offset, Size, Next, Total int32
}

func PageFor(ids []string, boundary *int64, offset, size int32) (Page, error) { return Page{}, nil }
