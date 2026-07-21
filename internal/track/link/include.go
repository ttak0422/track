package link

import (
	"regexp"
	"strconv"
	"strings"
)

// Include is one ![[...]] transclusion directive: a whole line that embeds another note's content
// (or one heading section of it) at this position. The link part shares the [[...]] grammar — key,
// heading anchor, display alias — so an include is also a plain Ref to the link graph; the "!"
// prefix and the trailing Org-style options are what make it an embed. Like Refs, extraction is
// store-free: resolving Text to a note and fetching its body is the caller's job.
type Include struct {
	Ref
	OnlyContents bool        // :only-contents — drop the matched heading line, keep its body
	Lines        []LineRange // :lines 4-5,8 — 1-based ranges over the extracted region, in given order
	BadOptions   []string    // unknown option keys and malformed values, for caller diagnostics
}

// LineRange is one 1-based inclusive range from a :lines option; a single line N is N-N.
type LineRange struct {
	From, To int
}

// includeLine matches a whole line that is exactly "![[...]]" plus optional trailing options —
// includes are block-level, so an embed in running text is not a directive.
var includeLine = regexp.MustCompile(`^!\[\[([^\[\]]+)\]\][ \t]*(.*)$`)

// Includes extracts every block-level ![[...]] directive in text, skipping fenced code blocks.
// The returned Ref carries the line number and the inner-text byte offsets within that line
// (matching what Refs reports for the same [[...]]), so consumers can key on either.
func Includes(text string) []Include {
	var out []Include
	for _, line := range scannableLines(text) {
		trimmed := strings.TrimLeft(line.Text, " \t")
		m := includeLine.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		indent := len(line.Text) - len(trimmed)
		target, display := splitDisplay(m[1])
		anchor := splitAnchor(target)
		if anchor.Key == "" {
			continue
		}
		inc := Include{Ref: Ref{
			Line:         line.Number,
			OpenByte:     indent + 1, // the "[[" just after "!"
			StartByte:    indent + 3,
			EndByte:      indent + 3 + len(m[1]),
			CloseByte:    indent + 3 + len(m[1]) + 2,
			Text:         anchor.Key,
			Display:      display,
			Heading:      anchor.Heading,
			HeadingLevel: anchor.HeadingLevel,
			BlockID:      anchor.BlockID,
		}}
		parseIncludeOptions(&inc, m[2])
		out = append(out, inc)
	}
	return out
}

// parseIncludeOptions reads the Org-style ":key value" tail of an include line (the same shape as
// babel header arguments). Unknown keys and malformed values land in BadOptions instead of being
// silently ignored, so a typo can surface as a diagnostic.
func parseIncludeOptions(inc *Include, tail string) {
	fields := strings.Fields(tail)
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case ":only-contents":
			inc.OnlyContents = true
		case ":lines":
			if i+1 >= len(fields) || strings.HasPrefix(fields[i+1], ":") {
				inc.BadOptions = append(inc.BadOptions, ":lines")
				continue
			}
			i++
			ranges, ok := parseLineRanges(fields[i])
			if !ok {
				inc.BadOptions = append(inc.BadOptions, ":lines "+fields[i])
				continue
			}
			inc.Lines = append(inc.Lines, ranges...)
		default:
			inc.BadOptions = append(inc.BadOptions, fields[i])
		}
	}
}

// parseLineRanges parses the ":lines" value: a comma-separated list of 1-based lines and inclusive
// ranges, e.g. "4", "4-5", "4-5,8" — the same range syntax babel's :visible-lines uses.
func parseLineRanges(s string) ([]LineRange, bool) {
	var out []LineRange
	for part := range strings.SplitSeq(s, ",") {
		from, to, ok := strings.Cut(part, "-")
		a, err := strconv.Atoi(from)
		if err != nil || a < 1 {
			return nil, false
		}
		b := a
		if ok {
			b, err = strconv.Atoi(to)
			if err != nil || b < a {
				return nil, false
			}
		}
		out = append(out, LineRange{From: a, To: b})
	}
	return out, len(out) > 0
}

// Extract returns the lines of body that inc selects: the whole note, with a block anchor the
// marked block (its "^id" marker stripped), or — with a heading anchor — the section from the
// matched heading through the line before the next heading of the same or a shallower level
// (headings inside fenced code blocks do not terminate a section, mirroring how they are not
// anchor targets). :only-contents drops the matched heading line; :lines then selects 1-based
// ranges over what remains, concatenated in the order written, out-of-range parts clipped.
// Leading and trailing blank lines are trimmed so the embed sits tight. ok is false when the anchor
// does not match any heading or block marker — unlike navigation (which falls back to the note
// top), an include must not silently embed the whole note.
func Extract(body string, inc Include) (lines []string, ok bool) {
	all := strings.Split(body, "\n")
	region := all
	if inc.BlockID != "" {
		from, to, found := FindBlock(body, inc.BlockID)
		if !found {
			return nil, false
		}
		region = append([]string(nil), all[from:to]...)
		for i := range region {
			region[i] = StripBlockMarker(region[i])
		}
	}
	if inc.Heading != "" {
		start, found := FindHeading(body, inc.HeadingLevel, inc.Heading)
		if !found {
			return nil, false
		}
		end := len(all)
		for _, h := range Headings(body) {
			if h.Line > start && h.Level <= inc.HeadingLevel {
				end = h.Line
				break
			}
		}
		region = all[start:end]
		if inc.OnlyContents {
			region = region[1:]
		}
	}
	if len(inc.Lines) > 0 {
		var picked []string
		for _, r := range inc.Lines {
			from, to := r.From-1, r.To
			if from >= len(region) {
				continue
			}
			if to > len(region) {
				to = len(region)
			}
			picked = append(picked, region[from:to]...)
		}
		region = picked
	}
	return trimBlankEdges(region), true
}

func trimBlankEdges(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}
