package webui

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// distFS holds the built React frontend. The directory is a committed placeholder in the source tree
// (so `go build` and `go test ./...` always compile); the real assets are produced by the web build
// (Vite) and copied into internal/track/webui/dist before the binary is built. See flake.nix.
//
//go:embed dist
var distFS embed.FS

// embeddedWebRoot serves the frontend with the embedded "dist" prefix stripped, so "/assets/app.js"
// maps to "dist/assets/app.js".
var embeddedWebRoot = mustSub(distFS, "dist")

var indexAssetPattern = regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`)

// selectWebRoot serves a local Vite build when track is run directly from the source tree. Nix builds
// still copy the real frontend into the embedded dist before compiling, and installed binaries normally
// fall back to embeddedWebRoot because they do not run from the repository root.
func selectWebRoot() fs.FS {
	const devDist = "web/dist"
	if isBuiltFrontendDist(devDist) {
		return os.DirFS(devDist)
	}
	return embeddedWebRoot
}

func isBuiltFrontendDist(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		return false
	}
	index := string(raw)
	if !strings.Contains(index, `type="module"`) {
		return false
	}
	assetRefs := indexAssetPattern.FindAllStringSubmatch(index, -1)
	if len(assetRefs) == 0 {
		return false
	}
	for _, match := range assetRefs {
		assetPath := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(match[1], "/")))
		stat, err := os.Stat(assetPath)
		if err != nil || stat.IsDir() {
			return false
		}
	}
	return true
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
