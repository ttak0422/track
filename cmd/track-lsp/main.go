// Command track-lsp is the Language Server Protocol frontend for track.
package main

import (
	"os"

	tracklsp "github.com/ttak0422/track/internal/track/lsp"
)

func main() {
	if err := tracklsp.Run(os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}
