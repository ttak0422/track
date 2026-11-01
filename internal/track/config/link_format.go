package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/vaultfile"
	"gopkg.in/yaml.v3"
)

// auditLinkFormat is deliberately read-only and runs on every load: activation
// must not reinterpret old titles, and a subsequently synced sidecar must fail
// closed instead of silently changing a link's destination.
func auditLinkFormat(vault, format string, registry map[string]string) error {
	if format != link.IDFormat {
		return nil
	}
	if _, exists := registry["id"]; exists {
		return fmt.Errorf("link_format id-v1 reserves vault name %q; rename that registry entry before enabling it", "id")
	}
	return auditLinkTitles(vault, format)
}

// ValidateLinkFormat rechecks the local namespace in a long-lived process. Remote
// title/ID ambiguity is checked only when that target is resolved, so an unrelated
// unavailable registered vault cannot block local work.
func (c *Config) ValidateLinkFormat() error {
	return auditLinkFormat(c.VaultDir, c.LinkFormat, c.Vaults)
}

// ValidateExplicitTarget checks the addressed target's on-disk namespace without
// rebuilding its index. Remote indexes may be stale after an external sync.
func (c *Config) ValidateExplicitTarget(key, sourceFormat string) error {
	if _, explicit := link.ExplicitID(key, sourceFormat); !explicit {
		return nil
	}
	return readLinkTitles(c.VaultDir, false, func(title string) error {
		if strings.TrimSpace(title) == key {
			return fmt.Errorf("ambiguous explicit ID %q: target vault also has that literal title; rename it before using this ID link", key)
		}
		return nil
	})
}

func auditLinkTitles(vault, format string) error {
	return readLinkTitles(vault, true, func(title string) error { return ValidateLinkTitle(format, title) })
}

// Startup can isolate invalid inputs; explicit ID resolution must still verify the target namespace.
func readLinkTitles(vault string, skipInvalid bool, validate func(string) error) error {
	dir := filepath.Join(vault, ".track", "notes")
	entries, err := vaultfile.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("audit link_format id-v1: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		if _, err := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".yaml"), 10, 64); err != nil {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := vaultfile.Read(path)
		if err != nil {
			var local *vaultfile.Error
			if skipInvalid && errors.As(err, &local) {
				vaultfile.Warn(local.Path, local.Reason)
				continue
			}
			return fmt.Errorf("audit link_format id-v1: %w", err)
		}
		var meta struct {
			Title string `yaml:"title"`
		}
		if err := yaml.Unmarshal(data, &meta); err != nil {
			if skipInvalid {
				vaultfile.Warn(path, "invalid note metadata")
				continue
			}
			return fmt.Errorf("audit link_format id-v1: %s: cannot verify title namespace (invalid metadata)", path)
		}
		if err := validate(meta.Title); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// ValidateLinkTitle prevents newly created or externally edited titles from
// colliding with the ID namespace. Qualifier-shaped titles are also rejected,
// including currently unregistered qualifiers that could be registered later.
func ValidateLinkTitle(format, title string) error {
	if format != link.IDFormat {
		return nil
	}
	key := strings.TrimSpace(title)
	if strings.HasPrefix(key, "id:") || strings.Contains(key, ":id:") {
		return fmt.Errorf("link_format id-v1 reserves id: targets; rename title %q before enabling or using this mode", title)
	}
	return nil
}
