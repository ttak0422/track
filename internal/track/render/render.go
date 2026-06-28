// Package render turns a resolved View Spec into concrete output.
//
// Renderers are pluggable: a Renderer maps a viewspec.Resolved onto one output format (Chart.js HTML
// today; SVG or D3 later) and registers under a name. Because the spec and the data resolution live
// upstream in viewspec, adding a renderer never touches the data model or the spec — which is the whole
// point of keeping the View Spec renderer-independent (see docs/adr/0021).
package render

import (
	"fmt"
	"sort"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// Renderer turns a resolved spec into a self-contained document (e.g. an HTML page).
type Renderer interface {
	// Name is the stable identifier used to select the renderer (e.g. "chartjs").
	Name() string
	// Render produces the output document for a resolved spec.
	Render(res viewspec.Resolved) (string, error)
}

// registry holds the available renderers by name. The MVP registers chartjs in this package's init.
var registry = map[string]Renderer{}

// Register adds a renderer, panicking on a duplicate name so collisions surface at init, not runtime.
func Register(r Renderer) {
	if _, dup := registry[r.Name()]; dup {
		panic(fmt.Sprintf("render: renderer %q already registered", r.Name()))
	}
	registry[r.Name()] = r
}

// Get returns the renderer registered under name, or an error listing what is available.
func Get(name string) (Renderer, error) {
	r, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown renderer %q (available: %v)", name, Names())
	}
	return r, nil
}

// Names lists the registered renderer names in sorted order, for help text and errors.
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
