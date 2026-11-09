package main

import (
	"fmt"
	"io"
	"runtime"
)

// runVersion prints the version the binary was built with, the Go release
// and the platform, for example "natslens v0.1.0 (go1.27.3 linux/amd64)".
func runVersion(args []string, w io.Writer) error {
	if len(args) > 0 {
		return fmt.Errorf("version takes no arguments, got %q", args)
	}
	_, err := fmt.Fprintf(w, "natslens %s (%s %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return err
}
