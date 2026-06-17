// Package builtin holds the templates shipped with track. They live at the repository root so they are
// easy to find and edit, and are embedded into the binary (go:embed cannot reach outside a package
// directory) so track can apply them without writing anything into the vault. A user template of the
// same name takes precedence; see internal/cli for resolution.
package builtin

import "embed"

// Templates holds the *.template.md files in this directory.
//
//go:embed *.template.md
var Templates embed.FS
