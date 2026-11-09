//go:build !(js && wasm)

package main

import (
	"fmt"
	"os"
)

// main says how to build the program: it runs only in a web page.
func main() {
	fmt.Fprintln(os.Stderr, "natslens-wasm runs in a web page: build it with GOOS=js GOARCH=wasm (make demo-site)")
	os.Exit(2)
}
