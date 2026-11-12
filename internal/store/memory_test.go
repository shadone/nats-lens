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

func TestSetThreads(t *testing.T) {
	s := openMem(t)
	if err := s.SetThreads(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	var got int64
	if err := s.DB().QueryRow(`SELECT current_setting('threads')`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("threads = %d", got)
	}
	if err := s.SetThreads(context.Background(), 0); err == nil {
		t.Fatal("threads 0 accepted")
	}
}

func TestSetMaxTempSize(t *testing.T) {
	s := openMem(t)
	if err := s.SetMaxTempSize(context.Background(), 64<<20); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := s.DB().QueryRow(`SELECT current_setting('max_temp_directory_size')`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "64.0 MiB" {
		t.Fatalf("max_temp_directory_size = %q", got)
	}
	if s.MaxTempSize() != 64<<20 {
		t.Fatalf("MaxTempSize = %d", s.MaxTempSize())
	}
	if err := s.SetMaxTempSize(context.Background(), 0); err == nil {
		t.Fatal("max_temp_directory_size 0 accepted")
	}
}
