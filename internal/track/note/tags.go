package note

import "regexp"

// inlineTagRe matches a hierarchical #tag in prose: "#proj/track", "#golang". The first segment
// must start with a letter or underscore, so a Markdown ATX heading ("# Heading", "## Foo") and a
// bare number ("#2026") are never tags; later segments accept the same loose [A-Za-z0-9_-] charset
// the sidecar already stores. The '#' must not be preceded by a word character or another '#', so
// "foo#bar" and "c#sharp" are not tags (RE2 has no lookbehind, so the boundary is a consumed
// non-capturing group; submatch 1 is the tag). A trailing separator ("#a/") drops the empty segment,
// matching how the query evaluator trims "#a/" (no tag is ever stored with one). Hex colors like
// "#ff0000" in prose are picked up the way Obsidian does — prose about colors usually quotes them
// in code.
var inlineTagRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_#])#([A-Za-z_][A-Za-z0-9_-]*(?:/[A-Za-z0-9_-]+)*)`)

// InlineTags scans a note body for hierarchical #tags outside fenced code blocks and inline code
// spans, returning them in first-seen order with duplicates dropped. It is the body half of
// CollectTags, mirroring how InlineFields is the body half of CollectProps: the same scanProse
// fence walk gates every scan, and inline code is masked before matching.
func InlineTags(body string) []string {
	var out []string
	seen := map[string]bool{}
	scanProse(body, func(line string, _ int) {
		// Mask `inline code` first, same as InlineFields, so a documented "#tag" example in a code
		// span never becomes data.
		line = codeSpanRe.ReplaceAllString(line, "``")
		for _, m := range inlineTagRe.FindAllStringSubmatch(line, -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			out = append(out, m[1])
		}
	})
	return out
}

// CollectTags returns every tag of a note — sidecar tags first, then inline #tags in body order —
// deduplicated by DedupTags with sidecar precedence. It is the one flattening the indexer's tags
// table uses, the role CollectProps plays for properties, so the query engine and search see the
// same tag set regardless of where a tag was written.
func CollectTags(meta Metadata, body string) []string {
	tags := make([]string, 0, len(meta.Tags)+8)
	tags = append(tags, meta.Tags...)
	tags = append(tags, InlineTags(body)...)
	return DedupTags(tags)
}
