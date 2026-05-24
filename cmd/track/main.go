// Command track is the note-tool CLI. It is a thin entry point; all logic lives
// in internal/cli (routing) and the internal/track/* engine packages, which a
// future LSP server can reuse directly.
package main

import (
	"os"

	"github.com/ttak0422/track/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
