package main

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestVersionPrintsBuildVersion(t *testing.T) {
	old := version
	version = "v1.2.3"
	t.Cleanup(func() { version = old })
	var out bytes.Buffer
	if err := runVersion(nil, &out); err != nil {
		t.Fatal(err)
	}
	want := "natslens v1.2.3 (" + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH + ")\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestVersionRejectsArguments(t *testing.T) {
	var out bytes.Buffer
	if err := runVersion([]string{"extra"}, &out); err == nil || !strings.Contains(err.Error(), "no arguments") {
		t.Fatalf("got %v, want an error about arguments", err)
	}
}

// The version command is dispatched before any configuration is read, so
// it works without a configuration file or NATS.
func TestRunDispatchesVersion(t *testing.T) {
	if err := run(context.Background(), []string{"version"}); err != nil {
		t.Fatal(err)
	}
}
