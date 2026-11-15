package calendarevent

import (
	_ "embed"
	"strings"
	"time"
)

// Keep the accepted identifiers independent of host-only zoneinfo files. The
// pinned Go distribution supplies IANA names and backward-compatible aliases;
// Factory is a placeholder, not an authored event zone. Browser support may lag.
// Regenerate when upgrading Go: go generate ./internal/track/calendarevent
//
//go:generate go run generate_zones.go
//go:embed zones.txt
var zoneNames string

var eventZones = func() map[string]bool {
	zones := make(map[string]bool)
	for _, name := range strings.Fields(zoneNames) {
		zones[name] = true
	}
	return zones
}()

// DisplayLocation validates the explicit display-zone contract shared by calendar projections.
func DisplayLocation(zone string) (*time.Location, error) {
	if len(zone) > 128 || !eventZones[zone] {
		return nil, invalid("time_zone must be a known explicit IANA identifier (maximum 128 bytes)")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, invalid("unknown IANA time_zone %q", zone)
	}
	return loc, nil
}
