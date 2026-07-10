// Package babel parses Org-Babel-style source blocks out of Markdown notes.
// Authoring stays in ordinary fenced code blocks; execution options ride in the fence info string as
// Org-style ":key value" header arguments (see docs/spec/babel.md). This package is the pure parsing
// layer: it extracts blocks and their header arguments and is deliberately free of execution, storage,
// and CLI concerns so it stays unit-testable. Resolving a block to a stable result key needs the note
// id, so that lives in ID rather than in the parser.
package babel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Block is one fenced code block carrying a language, so it is a candidate for Babel evaluation.
// Plain fenced blocks with no language are skipped and never become Blocks.
type Block struct {
	Language   string              // first info-string token
	Name       string              // value of :name, empty when unnamed
	HeaderArgs map[string][]string // ":key v1 v2" -> {"key": ["v1","v2"]}; repeated keys (e.g. :var) accumulate
	Body       string              // code between the fences, without the fence lines
	BodyHash   string              // "sha256:<hex>" of Body, for cache keys and result identity
	Fence      string              // the opening backtick run (e.g. "```" or "````"), so renderers reproduce it
	StartLine  int                 // 0-based line of the opening fence
	EndLine    int                 // 0-based line of the closing fence
	Ordinal    int                 // 0-based index among the note's language blocks
}

// ParseBlocks extracts every language-tagged fenced code block from a note body, in document order.
// Fences follow the CommonMark length rule: a block opened with N backticks is closed only by a bare
// run of at least N, so a ````markdown block can quote ``` fences without ending early.
// Unterminated fences end the scan. Body offsets are 0-based line numbers within body.
func ParseBlocks(body string) []Block {
	lines := strings.Split(body, "\n")
	var blocks []Block
	ordinal := 0
	i := 0
	for i < len(lines) {
		marker, info, ok := openFence(lines[i])
		if !ok {
			i++
			continue
		}
		start := i
		j := i + 1
		closed := false
		for j < len(lines) {
			if closesFence(lines[j], marker) {
				closed = true
				break
			}
			j++
		}
		if !closed {
			break // unterminated fence: ignore the rest
		}
		lang, args := parseInfoString(info)
		if lang != "" {
			blockBody := strings.Join(lines[start+1:j], "\n")
			blocks = append(blocks, Block{
				Language:   lang,
				Name:       firstValue(args, "name"),
				HeaderArgs: args,
				Body:       blockBody,
				BodyHash:   hashBody(blockBody),
				Fence:      marker,
				StartLine:  start,
				EndLine:    j,
				Ordinal:    ordinal,
			})
			ordinal++
		}
		i = j + 1
	}
	return blocks
}

// ID returns the stable result key for a block within a note.
// Named blocks use the name; unnamed blocks derive an id from note id, ordinal, language, and body hash.
func (b Block) ID(noteID int64) string {
	if b.Name != "" {
		return b.Name
	}
	return fmt.Sprintf("%d:%d:%s:%s", noteID, b.Ordinal, b.Language, shortHash(b.BodyHash))
}

// Validate rejects notes that reuse a block name, matching Org's requirement that source block names be unique.
func Validate(blocks []Block) error {
	seen := make(map[string]bool)
	for _, b := range blocks {
		if b.Name == "" {
			continue
		}
		if seen[b.Name] {
			return fmt.Errorf("duplicate babel block name %q", b.Name)
		}
		seen[b.Name] = true
	}
	return nil
}

// parseInfoString splits a fence info string into the language (first token) and Org-style header args.
// A ":key" token opens a key; following non-":" tokens are its values. Repeated keys accumulate values.
func parseInfoString(info string) (string, map[string][]string) {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return "", nil
	}
	args := make(map[string][]string)
	key := ""
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, ":") {
			key = strings.TrimPrefix(f, ":")
			if _, ok := args[key]; !ok {
				args[key] = []string{}
			}
			continue
		}
		if key != "" {
			args[key] = append(args[key], f)
		}
	}
	if len(args) == 0 {
		args = nil
	}
	return fields[0], args
}

func firstValue(args map[string][]string, key string) string {
	if vs := args[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func hashBody(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// shortHash returns a short, stable fragment of a "sha256:<hex>" string for use in generated ids.
func shortHash(bodyHash string) string {
	hexPart := strings.TrimPrefix(bodyHash, "sha256:")
	if len(hexPart) > 12 {
		return hexPart[:12]
	}
	return hexPart
}

// openFence reports whether line opens a fence: a run of at least three backticks, returned as
// marker, followed by the info string.
func openFence(line string) (marker, info string, ok bool) {
	t := strings.TrimSpace(line)
	n := 0
	for n < len(t) && t[n] == '`' {
		n++
	}
	if n < 3 {
		return "", "", false
	}
	return t[:n], strings.TrimSpace(t[n:]), true
}

// closesFence reports whether line closes a fence opened with marker: at least as many backticks
// and nothing but whitespace after them.
func closesFence(line, marker string) bool {
	t := strings.TrimSpace(line)
	n := 0
	for n < len(t) && t[n] == '`' {
		n++
	}
	return n >= len(marker) && strings.TrimSpace(t[n:]) == ""
}
