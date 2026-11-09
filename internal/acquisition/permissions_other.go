//go:build !darwin && !linux

package acquisition

import "errors"

func privateOwnedDirectory(path string) error {
	return errors.New("local acquisition requires Linux or macOS")
}
