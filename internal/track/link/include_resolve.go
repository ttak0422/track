package link

import "fmt"

// ResolvedInclude is the wire shape for one ![[...]] directive resolved against the vault: the
// live note API and the static-site bundle both emit it, so the web frontend renders includes
// identically in both modes. Line keys the directive to its 0-based body line; a directive that
// cannot embed still appears with Error so the renderer can mark the line instead of dropping it.
type ResolvedInclude struct {
	Line   int   `json:"line"`
	NoteID int64 `json:"note_id,omitempty"`
	// SourceLine is the 0-based line of the target note that Lines[0] came from, so a client can
	// map a line of the excerpt back to the file that owns it — what lets a task rendered through
	// an include be written back. -1 when the excerpt is not one contiguous run of the source.
	SourceLine int      `json:"source_line"`
	Kind       string   `json:"kind,omitempty"` // target file kind ("note"/"journal"), for asset resolution
	ETag       string   `json:"etag,omitempty"` // exact target version Lines were extracted from
	Title      string   `json:"title,omitempty"`
	Caption    string   `json:"caption"`
	Lines      []string `json:"lines"`
	BadOptions []string `json:"bad_options,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// ResolveIncludes extracts and resolves every include directive in body. load resolves a link key
// to the target note's id, file kind, and body text (ok=false when the key matches no note or the
// note cannot be read), injected so this stays store-free like the rest of the package.
func ResolveIncludes(body string, load func(key string) (id int64, kind, text, etag string, ok bool)) []ResolvedInclude {
	out := []ResolvedInclude{}
	for _, inc := range Includes(body) {
		res := ResolvedInclude{
			Line:       inc.Line,
			Caption:    inc.Display,
			Lines:      []string{},
			BadOptions: inc.BadOptions,
		}
		id, kind, text, etag, ok := load(inc.Text)
		if !ok {
			res.Error = fmt.Sprintf("unresolved note %q", inc.Text)
			out = append(out, res)
			continue
		}
		res.NoteID = id
		res.Kind = kind
		res.ETag = etag
		res.Title = inc.Text
		lines, start, ok := Extract(text, inc)
		if !ok {
			if inc.BlockID != "" {
				res.Error = fmt.Sprintf("block not found: ^%s", inc.BlockID)
			} else {
				res.Error = fmt.Sprintf("heading not found: %s", inc.Heading)
			}
			out = append(out, res)
			continue
		}
		res.Lines = lines
		res.SourceLine = start
		out = append(out, res)
	}
	return out
}
