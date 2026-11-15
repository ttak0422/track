//go:build !linux && !darwin

package timeline

import (
	"context"
	"errors"
	"os"
)

var errPlatform = errors.New("durable timelines currently require Linux or macOS filesystem locking")

func safeOpen(*os.Root, string, int, os.FileMode) (*os.File, error) { return nil, errPlatform }
func acquire(context.Context, *os.File) error                       { return errPlatform }
func release(*os.File)                                              {}
