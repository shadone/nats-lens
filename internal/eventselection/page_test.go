package eventselection

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSelectionPagesBoundWorkWithoutLosingMembership(t *testing.T) {
	ids := make([]string, 2000)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	for _, tc := range []struct{ offset, size, next, count int32 }{{0, 0, 50, 50}, {50, 25, 75, 25}, {1990, 50, 0, 10}, {1999, 1, 0, 1}} {
		p, err := PageFor(ids, nil, tc.offset, tc.size)
		if err != nil || p.Total != 2000 || p.Offset != tc.offset || p.Next != tc.next || len(p.IDs) != int(tc.count) {
			t.Fatalf("wrong bounded selection page: %+v %v", p, err)
		}
		if !reflect.DeepEqual(p.IDs, ids[tc.offset:tc.offset+tc.count]) {
			t.Fatal("selection order changed")
		}
	}
}
func TestSelectionPagesRejectInvalidInputs(t *testing.T) {
	negative := int64(-1)
	for _, tc := range []struct {
		ids          []string
		boundary     *int64
		offset, size int32
	}{
		{nil, nil, 0, 0}, {[]string{"1", "1"}, nil, 0, 0}, {[]string{"01"}, nil, 0, 0}, {[]string{"-1"}, nil, 0, 0}, {[]string{"9223372036854775808"}, nil, 0, 0}, {[]string{"1"}, &negative, 0, 0}, {[]string{"1"}, nil, -1, 0}, {[]string{"1"}, nil, 1, 0}, {[]string{"1"}, nil, 0, 51}, {[]string{"1"}, nil, 0, -1}, {make([]string, 2001), nil, 0, 0},
	} {
		if _, err := PageFor(tc.ids, tc.boundary, tc.offset, tc.size); err == nil {
			t.Fatalf("invalid selection admitted: offset=%d size=%d count=%d", tc.offset, tc.size, len(tc.ids))
		}
	}
}
