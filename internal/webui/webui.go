// Package webui holds the files of the web UI, embedded into the binary.
//
// `make web` (npm run build in web/) writes the UI into dist, which git
// ignores apart from .gitkeep. A binary built without that step serves the
// placeholder page instead, so that `go build` works without Node.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:embed placeholder
var placeholder embed.FS

// FS returns the UI files: the built UI when dist holds one, otherwise the
// placeholder page.
func FS() fs.FS {
	if built() {
		return sub(dist, "dist")
	}
	return sub(placeholder, "placeholder")
}

// built reports whether the binary embeds a built UI.
func built() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return err == nil
}

func sub(f embed.FS, dir string) fs.FS {
	s, err := fs.Sub(f, dir)
	if err != nil {
		panic(err) // dir is embedded at build time; Sub fails only for an invalid name
	}
	return s
}
