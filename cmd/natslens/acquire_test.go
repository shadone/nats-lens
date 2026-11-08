package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAcquireArgumentsAndHelp(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{nil, {"-socket", "/tmp/unused"}, {"-socket", "/tmp/unused", "-system", "shop-prod", "unexpected"}} {
		if err := runAcquire(context.Background(), args, &out); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	_ = runAcquire(context.Background(), []string{"-help"}, &out)
	for _, want := range []string{"all retained", "cooperative", "quota", "MVCC/WAL"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help omits %s", want)
		}
	}
}
