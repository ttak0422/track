// Command track is the note-tool CLI. It is a thin entry point; all logic lives
// in internal/cli (routing) and the internal/track/* engine packages, which a
// future LSP server can reuse directly.
package main

import (
	"context"
	"github.com/ttak0422/track/internal/track/vaultfile"
	"os"
	"time"

	"github.com/ttak0422/track/internal/cli"
)

func main() {
	code := cli.Run(os.Args[1:])
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = vaultfile.CloseWorkers(ctx)
	cancel()
	os.Exit(code)
}
