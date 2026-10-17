package store

import (
	"context"
	"testing"
)

func TestSetMemoryLimit(t *testing.T) {
	s := openMem(t)
	if err := s.SetMemoryLimit(context.Background(), 512<<20); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := s.DB().QueryRow(`SELECT current_setting('memory_limit')`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "512.0 MiB" {
		t.Fatalf("memory_limit = %q", got)
	}
}
