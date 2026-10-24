package site

import (
	"encoding/json"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/query"
)

// Published targets use the same opaque identity as published routes, not the
// source vault's numeric IDs. Unpublished targets stay unresolved, but opaque.
func publishedLinkKey(key, format, self string) string {
	if format != link.IDFormat {
		return key
	}
	prefix, local := "", key
	if before, after, ok := strings.Cut(key, ":"); ok && before != "id" {
		prefix, local = before+":", after
		if before == self {
			prefix = ""
		}
	}
	id, explicit := link.ExplicitID(local, format)
	if !explicit {
		return key
	}
	if id <= 0 {
		// An invalid source ID must never gain a destination just because it
		// happens to spell one of the published, nonnumeric aliases.
		return prefix + "id:" + publishSlug("unresolved-link:"+local)
	}
	return prefix + "id:" + PublishID(id)
}

func publishBodyLinks(body, format, self string) string {
	if format != link.IDFormat {
		return body
	}
	lines := strings.Split(body, "\n")
	refs := link.Refs(body)
	// Rewrite original spans once, right to left. A source key may happen to
	// spell another link's published alias, so sequential global replacements
	// could turn that invalid source reference into a valid link (or vice versa).
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		key := publishedLinkKey(ref.Text, format, self)
		if key == ref.Text {
			continue
		}
		line := lines[ref.Line]
		start := ref.StartByte + strings.Index(line[ref.StartByte:ref.EndByte], ref.Text)
		lines[ref.Line] = line[:start] + key + line[start+len(ref.Text):]
	}
	body = strings.Join(lines, "\n")
	// Non-table query layouts carry targets as structured data, not wiki links.
	return babel.ReplaceBlocks(body, query.ViewFenceLang, func(b babel.Block) []string {
		var view query.View
		if err := json.Unmarshal([]byte(b.Body), &view); err != nil {
			return lines[b.StartLine : b.EndLine+1]
		}
		changed := false
		for i := range view.Groups {
			for j := range view.Groups[i].Rows {
				row := &view.Groups[i].Rows[j]
				if target := publishedLinkKey(row.Target, format, self); target != row.Target {
					row.Target = target
					changed = true
				}
			}
		}
		if !changed {
			return lines[b.StartLine : b.EndLine+1]
		}
		data, err := json.Marshal(view)
		if err != nil {
			return lines[b.StartLine : b.EndLine+1]
		}
		return []string{"```" + query.ViewFenceLang, string(data), "```"}
	})
}
