//go:build !linux && !darwin

package config

import (
	"errors"
	"io/fs"
)

func manifestFileIdentity(info fs.FileInfo) (string, error) {
	return "", errors.New("launch manifest identity unsupported on this platform")
}
