// Package checkfiles holds the check files: one SQL file per check, named
// by its code, with a YAML header. internal/checks loads and runs them.
package checkfiles

import "embed"

// FS holds every check file.
//
//go:embed *.sql
var FS embed.FS
