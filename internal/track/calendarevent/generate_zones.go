//go:build ignore

// Generate the identifier list from the pinned Go toolchain's bundled IANA data.
// The IANA database is public domain: https://data.iana.org/time-zones/tz-link.html
package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func main() {
	archive, err := zip.OpenReader(filepath.Join(runtime.GOROOT(), "lib", "time", "zoneinfo.zip"))
	if err != nil {
		panic(err)
	}
	defer archive.Close()
	var names []string
	for _, file := range archive.File {
		if !file.FileInfo().IsDir() && file.Name != "Factory" {
			names = append(names, file.Name)
		}
	}
	sort.Strings(names)
	for _, path := range []string{"zones.txt", "../../../web/src/calendarTimeZones.txt"} {
		if err := os.WriteFile(path, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
			panic(err)
		}
	}
}
