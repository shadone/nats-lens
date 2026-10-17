package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateSystemDirs refuses two systems whose directories overlap: an
// archive equal to or inside another system's archive or data directory, a
// data directory inside another system's archive, or two equal data
// directories. An archive is listed recursively, so a nested one would be
// read and pruned by both systems. A data directory inside another one is
// allowed: each holds only its own files.
func validateSystemDirs(systems []System, valid []bool) []error {
	type dirs struct {
		who           string
		data, archive dirPath
	}
	var ds []dirs
	for i, s := range systems {
		if valid[i] {
			ds = append(ds, dirs{fmt.Sprintf("systems[%d] (%s)", i, s.ID), newDirPath(s.DataDir), newDirPath(s.ArchiveDir)})
		}
	}
	var errs []error
	for i := range ds {
		for j := i + 1; j < len(ds); j++ {
			if msg := dirClash(ds[i].data, ds[i].archive, ds[j].data, ds[j].archive); msg != "" {
				errs = append(errs, fmt.Errorf("%s and %s: %s", ds[i].who, ds[j].who, msg))
			}
		}
	}
	return errs
}

// dirClash describes how the directories of two systems a and b overlap,
// or returns "" when they do not.
func dirClash(aData, aArchive, bData, bArchive dirPath) string {
	switch {
	case aData.same(bData):
		return fmt.Sprintf("both use the same directory %s as data directory", aData.path)
	case aArchive.same(bArchive):
		return fmt.Sprintf("both use the same directory %s as archive directory", aArchive.path)
	case aArchive.within(bArchive) || bArchive.within(aArchive):
		return fmt.Sprintf("one archive directory is inside the other: %s and %s", aArchive.path, bArchive.path)
	case aArchive.within(bData):
		return fmt.Sprintf("the first's archive directory %s is inside the second's data directory %s", aArchive.path, bData.path)
	case bArchive.within(aData):
		return fmt.Sprintf("the second's archive directory %s is inside the first's data directory %s", bArchive.path, aData.path)
	case aData.within(bArchive):
		return fmt.Sprintf("the first's data directory %s is inside the second's archive directory %s", aData.path, bArchive.path)
	case bData.within(aArchive):
		return fmt.Sprintf("the second's data directory %s is inside the first's archive directory %s", bData.path, aArchive.path)
	}
	return ""
}

// dirPath is a directory as the file system resolves it: path with its
// symbolic links resolved, split into the longest part that exists and the
// rest that does not. Existing parts are compared with os.SameFile, so two
// spellings of one directory on a case-insensitive file system match. The
// rest is compared by its letters, so on such a file system two spellings
// of a directory that does not exist yet are not recognised as one.
type dirPath struct {
	path     string
	existing os.FileInfo // nil when nothing of the path could be read
	rest     string      // the part below the existing directory
}

func newDirPath(dir string) dirPath {
	p := realDir(dir)
	e, rest := p, ""
	for {
		if fi, err := os.Stat(e); err == nil {
			return dirPath{path: p, existing: fi, rest: rest}
		}
		parent := filepath.Dir(e)
		if parent == e {
			return dirPath{path: p}
		}
		rest = filepath.Join(filepath.Base(e), rest)
		e = parent
	}
}

// existingDir is the part of the path that exists.
func (d dirPath) existingDir() string {
	return strings.TrimSuffix(strings.TrimSuffix(d.path, d.rest), string(filepath.Separator))
}

// same reports whether d and o are one directory.
func (d dirPath) same(o dirPath) bool {
	if d.path == o.path {
		return true
	}
	return d.existing != nil && o.existing != nil && os.SameFile(d.existing, o.existing) && d.rest == o.rest
}

// within reports whether d is parent or inside it.
func (d dirPath) within(parent dirPath) bool {
	if within(d.path, parent.path) {
		return true
	}
	if d.existing == nil || parent.existing == nil {
		return false
	}
	if parent.rest != "" {
		return os.SameFile(d.existing, parent.existing) && within(d.rest, parent.rest)
	}
	for e := d.existingDir(); ; e = filepath.Dir(e) {
		if fi, err := os.Stat(e); err == nil && os.SameFile(fi, parent.existing) {
			return true
		}
		if filepath.Dir(e) == e {
			return false
		}
	}
}

// within reports whether dir is parent or inside it, by their letters.
func within(dir, parent string) bool {
	if dir == parent {
		return true
	}
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}
	return strings.HasPrefix(dir, parent)
}

// maxSymlinks bounds the links followed for one path, against loops.
const maxSymlinks = 255

// realDir returns dir as an absolute path with every symbolic link that
// exists resolved, component by component, so that ".." after a link
// leaves the link's target as the kernel does, not the link's directory.
func realDir(dir string) string {
	links := 0
	return resolvePath(dir, &links)
}

func resolvePath(dir string, links *int) string {
	sep := string(filepath.Separator)
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			return filepath.Clean(dir)
		}
		// Joined by hand: filepath.Join would apply ".." by the letters.
		dir = wd + sep + dir
	}
	cur := sep
	for _, part := range strings.Split(dir, sep) {
		switch part {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, part)
		fi, err := os.Lstat(next)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 || *links >= maxSymlinks {
			cur = next
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			cur = next
			continue
		}
		*links++
		if !filepath.IsAbs(target) {
			target = cur + sep + target
		}
		cur = resolvePath(target, links)
	}
	return cur
}
