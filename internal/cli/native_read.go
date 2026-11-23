package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/ttak0422/track/internal/track/webui"
)

// Internal owned-child transport, before any CLI configuration/selector rewrite.
// GPUI's ordinary-workspace admission remains a separate, closed boundary.
func cmdNativeRead(args []string) int {
	fs := flag.NewFlagSet("__native-read", flag.ContinueOnError)
	addr := fs.String("addr", "", "owned IPv4 loopback listener (serve only)")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	ctx, cancel, err := webServerContext()
	if err != nil {
		return fail("native read launch rejected")
	}
	defer cancel()
	onReady := func() {
		fmt.Fprintf(os.Stderr, "TRACK_DESKTOP_READY:%s\n", os.Getenv(desktopReadyTokenEnv))
	}
	if err := webui.RunBoundReadLaunch(ctx, os.Stdin, os.Stdout, *addr, onReady); err != nil {
		return fail("native read launch rejected")
	}
	return 0
}
