//go:build !linux && !darwin

package config

import "io/fs"

// No approximation of ownership/link count on an unaudited platform.
const cacheIdentitySupported = false

func cacheOwned(fs.FileInfo) bool      { return false }
func cacheSingleLink(fs.FileInfo) bool { return false }
func cacheUnlinked(fs.FileInfo) bool   { return false }
