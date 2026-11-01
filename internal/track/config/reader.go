package config

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/ttak0422/track/internal/track/vaultfile"
)

func init() {
	vaultfile.ServeConfigWorker(func(path string, strict bool) ([]byte, error) {
		if path == "." {
			path = ""
		}
		cfg, err := loadPolicy(path, strict)
		if err != nil {
			return nil, err
		}
		if err := validateReadRoot(cfg); err != nil {
			return nil, err
		}
		return json.Marshal(cfg)
	})
}

// LoadBounded isolates config reads/audits from listener startup. A provisional
// config is validated for reading but cannot authorize any mutations.
func LoadBounded(ctx context.Context, path string, strict bool) (*Config, error) {
	if path == "" {
		path = "."
	}
	raw, err := vaultfile.ConfigData(ctx, path, strict)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.VaultDir == "" || !filepath.IsAbs(cfg.VaultDir) || cfg.DBPath == "" {
		return nil, errors.New("invalid configuration worker response")
	}
	return &cfg, nil
}

// This validation executes inside the owned configuration worker, so it shares
// its deadline without competing with the note-reader pool during cold startup.
func validateReadRoot(cfg *Config) error {
	info, err := vaultfile.Stat(cfg.VaultDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("vault root is not a directory")
	}
	entries, err := vaultfile.ReadDir(cfg.VaultDir)
	if err != nil {
		return err
	}
	empty, recognized := true, false
	names := map[string]bool{".track": true}
	for _, dir := range cfg.VaultSkeleton() {
		names[filepath.Base(dir)] = true
	}
	for _, entry := range entries {
		if entry.Name() != ".DS_Store" {
			empty = false
		}
		if entry.IsDir() && names[entry.Name()] {
			recognized = true
		}
	}
	if !empty && !recognized {
		return errors.New("configured directory is not a Track vault; initialize it explicitly")
	}
	return nil
}
