// Command track is a dummy tracker CLI scaffold.
//
// It exposes the minimum surface the Neovim plugin needs: a version probe and
// a `dump` command whose output the plugin renders in a buffer. The dump
// payload is a placeholder until the real tracking state lands.
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func usage() {
	fmt.Fprint(os.Stderr, `track - dummy tracker

Usage:
  track dump      # print the current state as JSON
  track version   # print the version
`)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 1
	}

	switch args[0] {
	case "dump":
		fmt.Printf("{\n  \"version\": %q,\n  \"entries\": []\n}\n", version)
	case "version", "--version", "-v":
		fmt.Printf("track %s\n", version)
	default:
		fmt.Fprintf(os.Stderr, "track: unknown command %q\n", args[0])
		usage()
		return 1
	}

	return 0
}
