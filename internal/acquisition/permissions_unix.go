//go:build darwin || linux

package acquisition

import (
	"fmt"
	"os"
	"syscall"
)

func privateOwnedDirectory(path string) error {
	i, err := os.Lstat(path)
	if err != nil || !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("acquisition directory must exist, be private, and not be a symlink")
	}
	stat, ok := i.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("acquisition directory must be owned by this user")
	}
	return nil
}
