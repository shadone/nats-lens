//go:build darwin

package dataset

import "golang.org/x/sys/unix"

func renameNew(from, to string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_EXCL)
}
func freeBytes(path string) (int64, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return 0, err
	}
	return int64(s.Bavail) * int64(s.Bsize), nil
}
