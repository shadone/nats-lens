package collect

import (
	"reflect"
	"testing"
	"time"
)

// A reply that sends one enum field twice, under keys that differ in case,
// reports both refused values in the same order on every decode, although
// the decoder's map does not keep their order.
func TestUnknownValuesOfOneFieldInAFixedOrder(t *testing.T) {
	b := []byte(`{"server":{"id":"N1"},"data":{"account_details":[{"stream_detail":[{"config":{"Retention":"1008","retention":"0A189"}}]}]}}`)
	var first []UnknownValue
	for i := range 50 {
		snap := NewSnapshot(time.Time{})
		if err := snap.Apply(Jsz, b); err != nil {
			t.Fatal(err)
		}
		got := snap.Servers["N1"].Status[Jsz].Unknown
		if len(got) != 2 {
			t.Fatalf("unknown = %+v, want both values", got)
		}
		if i == 0 {
			first = got
		} else if !reflect.DeepEqual(got, first) {
			t.Fatalf("decode %d reports %+v, the first %+v", i, got, first)
		}
	}
}
