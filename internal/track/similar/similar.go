// Package similar computes semantic related-notes. The heavy lifting — turning text into a vector — lives
// OUTSIDE the engine, in a user-configured embedder command (same split as the track-fetch-* tools): the
// engine feeds a note's text on stdin and reads a JSON array of floats on stdout. Vectors are cached in
// the index DB keyed by note + content hash, so an unchanged note is never re-embedded, and nearest
// notes are found by cosine similarity. With no embedder configured, nothing here runs and no other
// command is affected.
package similar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// embedTimeout bounds one embedder invocation so a hung command cannot hang `track similar` forever.
// 120s is deliberately generous: a local model's first call may spend tens of seconds just loading
// weights. A var (not a const) so tests can shrink it.
// ponytail: fixed timeout; make it a config knob if real embedders need more.
var embedTimeout = 120 * time.Second

// EmbedFunc turns a note's text into a vector. It is the seam that keeps the model out of the engine:
// tests inject a deterministic fake, production wires CommandEmbedder to the configured command.
type EmbedFunc func(text string) ([]float32, error)

// Result is one related note, ranked by cosine similarity to the target (1 = identical direction). Path
// is left for the CLI layer to fill, matching SearchResult/NoteRef.
type Result struct {
	NoteID   int64   `json:"note_id"`
	FileKind string  `json:"file_kind"`
	Path     string  `json:"path,omitempty"`
	Title    string  `json:"title"`
	Score    float64 `json:"score"`
}

// CommandEmbedder wraps the configured embedder command as an EmbedFunc, or returns false when no
// embedder is configured. The command receives the note text on stdin and must print a JSON array of
// numbers on stdout, e.g. "[0.1, -0.2, 0.3]".
func CommandEmbedder(cfg *config.Config) (EmbedFunc, bool) {
	argv := cfg.EmbedderCommand
	if len(argv) == 0 {
		return nil, false
	}
	return func(text string) ([]float32, error) {
		ctx, cancel := context.WithTimeout(context.Background(), embedTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdin = strings.NewReader(text)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		// On deadline the embedder process is SIGKILLed, which unblocks the stdin-writing goroutine
		// with EPIPE. WaitDelay covers the remaining wedge: a grandchild that inherited the stdout/
		// stderr pipes and outlives the kill would otherwise block Wait indefinitely.
		cmd.WaitDelay = 5 * time.Second
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("%s: timed out after %s: %w", argv[0], embedTimeout, context.DeadlineExceeded)
			}
			if stderr.Len() > 0 {
				return nil, fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(stderr.String()))
			}
			return nil, fmt.Errorf("%s: %w", argv[0], err)
		}
		var vec []float32
		if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &vec); err != nil {
			return nil, fmt.Errorf("%s: output is not a JSON float array: %w", argv[0], err)
		}
		if len(vec) == 0 {
			return nil, fmt.Errorf("%s: returned an empty vector", argv[0])
		}
		return vec, nil
	}, true
}

// Ensure embeds every indexed note whose content hash differs from its cached hash, leaving unchanged
// notes untouched. The hash folds in the embedder signature, so swapping the embedder command re-embeds
// the whole vault instead of leaving vectors of a stale dimension in the cache. It returns how many notes
// were (re-)embedded.
func Ensure(cfg *config.Config, s *store.Store, embed EmbedFunc) (int, error) {
	notes, err := s.AllNotes()
	if err != nil {
		return 0, err
	}
	hashes, err := s.EmbeddingHashes()
	if err != nil {
		return 0, err
	}
	// NUL-joined so the signature stays injective: sequence-form argv elements may contain spaces, and
	// a space join would let two different commands share a signature and never trigger a re-embed.
	sig := strings.Join(cfg.EmbedderCommand, "\x00")

	embedded := 0
	for _, n := range notes {
		// Related-notes is a note-to-note feature: journals are date buckets whose content is whatever
		// notes were touched that day, so ranking them as "related" is noise. Skip them.
		if n.FileKind != config.KindNote {
			continue
		}
		parsed, err := note.ParseFile(cfg.PathForKind(n.FileKind, n.NoteID), cfg)
		if err != nil {
			return embedded, err
		}
		text := contentText(parsed)
		h := hashText(sig, text)
		if hashes[n.NoteID] == h {
			continue
		}
		vec, err := embed(text)
		if err != nil {
			return embedded, fmt.Errorf("embed note %d: %w", n.NoteID, err)
		}
		if err := s.UpsertEmbedding(n.NoteID, h, vec); err != nil {
			return embedded, err
		}
		embedded++
	}
	return embedded, nil
}

// Nearest ranks every embedded note against the target by cosine similarity, most similar first, and
// returns at most limit results. The target itself is excluded. It errors when the target has no cached
// vector (unknown id, or a note that could not be embedded).
func Nearest(all []store.Embedding, targetID int64, limit int) ([]Result, error) {
	var target []float32
	found := false
	for _, e := range all {
		if e.NoteID == targetID {
			target = e.Vector
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("note %d has no embedding", targetID)
	}

	// ponytail: brute-force O(n·d) cosine scan over every cached vector. Fine to a few thousand notes;
	// swap for an approximate-nearest-neighbour index (an hnsw sidecar, or sqlite-vec) past that.
	out := make([]Result, 0, len(all))
	for _, e := range all {
		if e.NoteID == targetID {
			continue
		}
		out = append(out, Result{
			NoteID:   e.NoteID,
			FileKind: e.FileKind,
			Title:    e.Title,
			Score:    cosine(target, e.Vector),
		})
	}
	slices.SortFunc(out, func(a, b Result) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		return int(a.NoteID - b.NoteID) // stable, deterministic tie-break
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// contentText is what gets embedded: the authoritative title followed by the body. Titles carry meaning,
// so folding them in keeps two notes on the same subject close even when their bodies diverge.
func contentText(n *note.Note) string {
	title := strings.TrimSpace(n.Meta.Title)
	if title == "" {
		return n.Body
	}
	return title + "\n\n" + n.Body
}

// hashText is the cache key: the embedder signature plus the note text. Same signature + same text ⇒ same
// hash ⇒ no re-embed; either changing invalidates the cached vector.
func hashText(sig, text string) string {
	sum := sha256.Sum256([]byte(sig + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

// cosine returns the cosine similarity of two vectors, 0 when either is zero-length or a zero vector, or
// when their dimensions disagree (a corrupt cache entry scores 0 rather than crashing the scan).
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
