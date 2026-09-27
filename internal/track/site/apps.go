package site

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ttak0422/track/internal/track/staticapp"
)

// publishApps replaces the reserved output apps/ tree with exactly the selected apps. Unlike vault
// attachments these files keep their names and bytes: relative browser dependencies use the same
// directory layout as the source app.
func publishApps(vaultDir, outDir string, names []string) error {
	dstRoot := filepath.Join(outDir, staticapp.DirName)
	if err := os.RemoveAll(dstRoot); err != nil {
		return err
	}
	for _, name := range names {
		root, err := staticapp.Open(vaultDir, name)
		if err != nil {
			return fmt.Errorf("open app %q: %w", name, err)
		}
		if err := staticapp.CheckIndex(root); err != nil {
			root.Close()
			return fmt.Errorf("app %q: %w", name, err)
		}
		err = staticapp.Copy(root, filepath.Join(dstRoot, name))
		root.Close()
		if err != nil {
			return fmt.Errorf("copy app %q: %w", name, err)
		}
	}
	return nil
}
