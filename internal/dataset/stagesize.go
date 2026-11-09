package dataset

import (
	"errors"
	"io/fs"
)

// stageSize adds up the sizes of the files in fsys. DuckDB removes its
// write-ahead log at a checkpoint, so a file listed in a directory can be
// gone before its size is read; such a file counts as empty.
func stageSize(fsys fs.FS) (int64, error) {
	var size int64
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			if p != "." && errors.Is(e, fs.ErrNotExist) {
				return nil
			}
			return e
		}
		if d.IsDir() {
			return nil
		}
		i, e := d.Info()
		if errors.Is(e, fs.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		size += i.Size()
		return nil
	})
	return size, err
}
