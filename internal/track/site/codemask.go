package site

import "strings"

// maskCode returns a copy of body in which every byte inside a fenced code block or an inline code span
// is replaced by a space, leaving newlines and all other bytes in place. Offsets and length are
// preserved, so a regular-expression match found in the masked copy indexes straight into the original
// body. The asset-reference scan uses this so an "assets/<file>" written as a literal example in code
// (e.g. the embed examples in the bundled help) is never mistaken for a real attachment and rewritten.
func maskCode(body string) string {
	out := []byte(body)
	var fenceChar byte
	var fenceLen int
	inFence := false

	for i, n := 0, len(body); i < n; {
		end := i
		for end < n && body[end] != '\n' {
			end++
		}
		line := body[i:end]
		switch {
		case inFence:
			maskRange(out, i, end)
			if c, l, rest, ok := fenceInfo(line); ok && c == fenceChar && l >= fenceLen && strings.TrimSpace(line[rest:]) == "" {
				inFence = false
			}
		default:
			if c, l, _, ok := fenceInfo(line); ok {
				maskRange(out, i, end)
				inFence = true
				fenceChar = c
				fenceLen = l
			} else {
				maskInlineSpans(out, i, line)
			}
		}
		i = end + 1
	}
	return string(out)
}

// fenceInfo reports whether line opens or closes a code fence: a run of at least three "`" or "~" after
// up to three leading spaces. It returns the fence character, the run length, and the offset just past
// the run (where a closing fence must hold only trailing whitespace).
func fenceInfo(line string) (char byte, length, rest int, ok bool) {
	k := 0
	for k < len(line) && k < 3 && line[k] == ' ' {
		k++
	}
	if k >= len(line) || (line[k] != '`' && line[k] != '~') {
		return 0, 0, 0, false
	}
	c := line[k]
	start := k
	for k < len(line) && line[k] == c {
		k++
	}
	if k-start < 3 {
		return 0, 0, 0, false
	}
	return c, k - start, k, true
}

// maskInlineSpans masks each inline code span in line (a run of N backticks closed by another run of
// exactly N backticks). base is line's byte offset within the body that out backs. An unterminated run
// of backticks is literal text and left untouched.
func maskInlineSpans(out []byte, base int, line string) {
	i := 0
	for i < len(line) {
		if line[i] != '`' {
			i++
			continue
		}
		runStart := i
		for i < len(line) && line[i] == '`' {
			i++
		}
		runLen := i - runStart
		closeEnd := -1
		for j := i; j < len(line); {
			if line[j] != '`' {
				j++
				continue
			}
			cs := j
			for j < len(line) && line[j] == '`' {
				j++
			}
			if j-cs == runLen {
				closeEnd = j
				break
			}
		}
		if closeEnd == -1 {
			continue
		}
		maskRange(out, base+runStart, base+closeEnd)
		i = closeEnd
	}
}

// maskRange overwrites out[start:end) with spaces, preserving newlines so line boundaries stay intact.
func maskRange(out []byte, start, end int) {
	for k := start; k < end; k++ {
		if out[k] != '\n' {
			out[k] = ' '
		}
	}
}
