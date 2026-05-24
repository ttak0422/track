package note

import (
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ttak0422/track/internal/track/config"
)

// SplitFootmatter separates a note's body from its footmatter block. It looks
// for the last footmatter block (open marker line ... close marker line) so a
// stray earlier comment does not confuse parsing. When no block is present the
// whole input is returned as the body with found=false.
func SplitFootmatter(raw string, m config.FootmatterMarkers) (body string, f Footmatter, found bool, err error) {
	lines := strings.Split(raw, "\n")
	openIdx, closeIdx := -1, -1
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if closeIdx == -1 {
			if t == m.Close {
				closeIdx = i
			}
			continue
		}
		if t == m.Open {
			openIdx = i
			break
		}
	}
	if openIdx == -1 || closeIdx == -1 || closeIdx <= openIdx {
		return strings.TrimRight(raw, "\n"), Footmatter{}, false, nil
	}

	yamlText := strings.Join(lines[openIdx+1:closeIdx], "\n")
	if err := yaml.Unmarshal([]byte(yamlText), &f); err != nil {
		return "", Footmatter{}, true, err
	}
	body = strings.TrimRight(strings.Join(lines[:openIdx], "\n"), "\n")
	return body, f, true, nil
}

// UpsertFootmatter replaces a note's footmatter block with one rendered from f,
// appending it after the body if none exists.
func UpsertFootmatter(raw string, f Footmatter, m config.FootmatterMarkers) (string, error) {
	body, _, _, err := SplitFootmatter(raw, m)
	if err != nil {
		return "", err
	}
	block, err := renderFootmatter(f, m)
	if err != nil {
		return "", err
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return block + "\n", nil
	}
	return body + "\n\n" + block + "\n", nil
}

func renderFootmatter(f Footmatter, m config.FootmatterMarkers) (string, error) {
	out, err := yaml.Marshal(f)
	if err != nil {
		return "", err
	}
	return m.Open + "\n" + strings.TrimRight(string(out), "\n") + "\n" + m.Close, nil
}
