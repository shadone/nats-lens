//go:build !linux && !darwin

package main

import "errors"

// statFree is not read on this platform; the derived spill bound applies
// unlowered.
func statFree(string) (int64, error) {
	return 0, errors.New("free disk space not read on this platform")
}
