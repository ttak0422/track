package babel

// BlockMeta is the stored, post-parse view of a source block: its language, normalized header
// arguments, the body hash it was last seen with, and the most recent run. It lives in the note
// sidecar metadata (schema version 2). Keeping it here, beside the parser, lets the note package
// embed it without the parser depending on note.
type BlockMeta struct {
	Language   string              `yaml:"language"`
	HeaderArgs map[string][]string `yaml:"header_args,omitempty"`
	BodyHash   string              `yaml:"body_hash,omitempty"`
	LastRun    *RunResult          `yaml:"last_run,omitempty"`
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
