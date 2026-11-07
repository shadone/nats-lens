package api_test

import "sync/atomic"

type atomicBool struct{ v atomic.Int32 }

func (b *atomicBool) toggle() bool { return b.v.Add(1)%2 == 0 }
