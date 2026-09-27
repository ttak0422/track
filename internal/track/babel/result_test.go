package babel

import "testing"

func TestStoredResultIntegrity(t *testing.T) {
	blocks := ParseBlocks("```sh :name a\necho one\n```\n```sh :name b :var x=a\necho $x\n```\n```sh :name c :var x=b\necho $x\n```\n")
	metadata := make(map[string]BlockMeta)
	for _, b := range blocks {
		vars, err := ResolveVars(b, nil, blocks, 1, metadata)
		if err != nil {
			t.Fatal(err)
		}
		meta := b.Meta()
		meta.InputHash = InputHash(b, b.Body, vars)
		meta.LastRun = &RunResult{Status: "success", Stdout: "one\n"}
		metadata[b.Name] = meta
	}
	if StoredResult(blocks[2], blocks, 1, metadata) == nil {
		t.Fatal("valid chain rejected")
	}
	old := metadata["a"]
	failed := old
	failed.LastRun = &RunResult{Status: "failed", Stdout: "partial"}
	metadata["a"] = failed
	if StoredResult(blocks[2], blocks, 1, metadata) != nil {
		t.Fatal("failed dependency accepted")
	}
	metadata["a"] = old
	blocks[0].Body = "echo two"
	if StoredResult(blocks[2], blocks, 1, metadata) != nil {
		t.Fatal("transitive stale result accepted")
	}
	blocks[0].HeaderArgs["var"] = []string{"x=c"}
	if StoredResult(blocks[2], blocks, 1, metadata) != nil {
		t.Fatal("cyclic dependency accepted")
	}
}

func TestExecutionKeyIncludesContext(t *testing.T) {
	b := ParseBlocks("```sh :var x=one :cache yes\necho $x\n```\n")[0]
	hash := InputHash(b, b.Body, map[string]string{"x": "one"})
	executor := Executor{Command: "sh", Args: []string{"{{file}}"}}
	key := ExecutionKey(hash, "/one", executor)
	for _, changed := range []string{
		ExecutionKey(hash, "/two", executor),
		ExecutionKey(hash, "/one", Executor{Command: "sh", Args: []string{"-e", "{{file}}"}}),
		ExecutionKey(InputHash(b, b.Body, map[string]string{"x": "two"}), "/one", executor),
	} {
		if changed == key {
			t.Fatal("changed execution context reused key")
		}
	}
	b.HeaderArgs = map[string][]string{"cache": {"yes"}, "var": {"x=one"}}
	if InputHash(b, b.Body, map[string]string{"x": "one"}) != hash {
		t.Fatal("header map order changes hash")
	}
}
