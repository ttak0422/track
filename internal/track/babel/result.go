package babel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// BlockMeta is the stored, post-parse view of a source block: its language, normalized header
// arguments, the body hash it was last seen with, and the most recent run. It lives in the note
// sidecar metadata (results in v2, input/execution hashes in v12). Keeping it here, beside the parser, lets the note package
// embed it without the parser depending on note.
type BlockMeta struct {
	Language     string              `yaml:"language"`
	HeaderArgs   map[string][]string `yaml:"header_args,omitempty"`
	BodyHash     string              `yaml:"body_hash,omitempty"`
	InputHash    string              `yaml:"input_hash,omitempty"`
	ExecutionKey string              `yaml:"execution_key,omitempty"`
	LastRun      *RunResult          `yaml:"last_run,omitempty"`
}

// RunResult captures one execution of a block.
// Times are RFC3339 strings so YAML round-trips them verbatim instead of reformatting a time.Time.
type RunResult struct {
	StartedAt  string   `yaml:"started_at,omitempty"`
	FinishedAt string   `yaml:"finished_at,omitempty"`
	Status     string   `yaml:"status,omitempty"`
	ExitCode   int      `yaml:"exit_code"`
	Stdout     string   `yaml:"stdout,omitempty"`
	Stderr     string   `yaml:"stderr,omitempty"`
	Value      string   `yaml:"value,omitempty"`
	Files      []string `yaml:"files,omitempty"`
}

// Meta returns the stored metadata view of a parsed block, without a run result.
func (b Block) Meta() BlockMeta {
	return BlockMeta{
		Language:   b.Language,
		HeaderArgs: b.HeaderArgs,
		BodyHash:   b.BodyHash,
	}
}

// DisplayResult distinguishes transient output (silent) from suppressed output (none/discard).
func DisplayResult(tokens []string) bool {
	for _, token := range tokens {
		if token == "none" || token == "discard" {
			return false
		}
	}
	return true
}

// InputHash identifies document inputs independently of the machine executing them.
// JSON sorts map keys, so header/variable map iteration cannot change the hash.
func InputHash(b Block, expanded string, vars map[string]string) string {
	return digest(struct {
		Language string
		Headers  map[string][]string
		Body     string
		Vars     map[string]string
	}{b.Language, b.HeaderArgs, expanded, vars})
}

// ExecutionKey additionally distinguishes the working directory and configured executor.
// External files and ambient process environment are deliberately not tracked by :cache.
func ExecutionKey(inputHash, dir string, executor Executor) string {
	return digest(struct {
		Input, Dir string
		Executor   Executor
	}{inputHash, dir, executor})
}

func digest(value any) string {
	data, _ := json.Marshal(value) // only strings, slices, and maps of strings are used here
	return hashBody(string(data))
}

// EvaluationBody expands references without changing a block's authoring identity.
func EvaluationBody(b Block, blocks []Block) (string, error) {
	if NowebExpands(b, "eval") {
		return ExpandNoweb(b.Body, blocks)
	}
	return b.Body, nil
}

// StoredResult returns a run only if its document inputs still match. Legacy records
// without an input hash cannot prove their inputs (including CLI overrides) and need a rerun.
// Failed runs may be displayed, but never serve as dependency inputs or cache hits.
func StoredResult(b Block, blocks []Block, noteID int64, metadata map[string]BlockMeta) *RunResult {
	return storedResult(b, blocks, noteID, metadata, map[string]bool{})
}

func storedResult(b Block, blocks []Block, noteID int64, metadata map[string]BlockMeta, visiting map[string]bool) *RunResult {
	if !DisplayResult(b.HeaderArgs["results"]) {
		return nil
	}
	id := b.ID(noteID)
	meta, ok := metadata[id]
	if !ok || meta.LastRun == nil || meta.InputHash == "" || meta.Language != b.Language || meta.BodyHash != b.BodyHash || visiting[id] {
		return nil
	}
	visiting[id] = true
	defer delete(visiting, id)
	expanded, err := EvaluationBody(b, blocks)
	if err != nil {
		return nil
	}
	vars, err := resolveVars(b, nil, blocks, noteID, metadata, visiting)
	if err != nil || meta.InputHash != InputHash(b, expanded, vars) {
		return nil
	}
	return meta.LastRun
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ResolveVars merges assignments before resolving references, so overridden references
// need no stored result. References never execute code; only current successful runs qualify.
func ResolveVars(b Block, overrides []string, blocks []Block, noteID int64, metadata map[string]BlockMeta) (map[string]string, error) {
	return resolveVars(b, overrides, blocks, noteID, metadata, map[string]bool{b.ID(noteID): true})
}

func resolveVars(b Block, overrides []string, blocks []Block, noteID int64, metadata map[string]BlockMeta, visiting map[string]bool) (map[string]string, error) {
	specs := append(append([]string{}, b.HeaderArgs["var"]...), overrides...)
	if len(specs) == 0 {
		return nil, nil
	}
	vars := make(map[string]string, len(specs))
	for _, spec := range specs {
		k, v, ok := strings.Cut(spec, "=")
		if !ok || !envName.MatchString(k) {
			return nil, fmt.Errorf("var %q: want key=value with a valid environment variable name", spec)
		}
		vars[k] = v
	}
	named := make(map[string]Block)
	for _, block := range blocks {
		if block.Name != "" {
			named[block.Name] = block
		}
	}
	for k, v := range vars {
		if dependency, ok := named[v]; ok {
			result := storedResult(dependency, blocks, noteID, metadata, visiting)
			if result == nil || result.Status != "success" {
				return nil, fmt.Errorf("var %s references block %q, which has no stored result with current successful inputs; run 'track babel exec --name %s' first", k, v, v)
			}
			if result.Value != "" {
				v = result.Value
			} else {
				v = result.Stdout
			}
			vars[k] = strings.TrimRight(v, "\n")
		} else if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			vars[k] = v[1 : len(v)-1]
		}
	}
	return vars, nil
}
