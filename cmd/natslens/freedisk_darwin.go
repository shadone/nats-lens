package main

import "golang.org/x/sys/unix"

// statFree returns the bytes free to an unprivileged user on the disk
// that holds path. On macOS Bsize is the fundamental block size the block
// counts are in; there is no Frsize.
func statFree(path string) (int64, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return 0, err
	}
	return int64(s.Bavail) * int64(s.Bsize), nil
}
