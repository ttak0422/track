//go:build !linux && !darwin

package config

import (
	"strings"
	"testing"
)

func TestReadOnlyCacheUnsupportedPlatformFailsClosed(t *testing.T) {
	path, err := ReadOnlyCachePath("", nil, nil)
	if path != "" || err == nil || !strings.Contains(err.Error(), "unsupported on this platform") {
		t.Fatalf("got %q %v", path, err)
	}
}
