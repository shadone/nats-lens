//go:build !darwin && !linux

package dataset

import "errors"

func renameNew(from, to string) error {
	return errors.New("atomic dataset publication requires Linux or macOS")
}
func freeBytes(path string) (int64, error) {
	return 0, errors.New("dataset acquisition requires Linux or macOS")
}
