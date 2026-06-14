package webui

import (
	"embed"
	"io/fs"
)

// distFS holds the built React frontend. The directory is a committed placeholder in the source tree
// (so `go build` and `go test ./...` always compile); the real assets are produced by the web build
// (Vite) and copied into internal/track/webui/dist before the binary is built. See flake.nix.
//
//go:embed dist
var distFS embed.FS

// webRoot serves the frontend with the embedded "dist" prefix stripped, so "/assets/app.js" maps to
// "dist/assets/app.js".
var webRoot = mustSub(distFS, "dist")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
