package main

import "golang.org/x/sys/unix"

// statFree returns the bytes free to an unprivileged user on the disk
// that holds path. Blocks are counted in the fragment size, Frsize, which
// statfs(2) gives the block counts in; Bsize is the preferred I/O size.
func statFree(path string) (int64, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return 0, err
	}
	size := s.Frsize
	if size == 0 {
		size = s.Bsize
	}
	return int64(s.Bavail) * size, nil
}
