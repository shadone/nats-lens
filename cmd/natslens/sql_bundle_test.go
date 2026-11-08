package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSQLBundleSelectionRejectsAmbiguity(t *testing.T) {
	for _, args := range [][]string{{"-bundle", "fixture", "-db", "closed.duckdb", "SELECT 1"}, {"-bundle", "fixture", "-config", "missing", "SELECT 1"}} {
		var out bytes.Buffer
		err := runSQLTo(context.Background(), args, &out)
		if err == nil || !strings.Contains(err.Error(), "exclusive") || out.Len() != 0 {
			t.Fatalf("%v: %v %s", args, err, out.String())
		}
	}
}
