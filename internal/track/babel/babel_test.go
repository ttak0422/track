package babel

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseBlocksBasic(t *testing.T) {
	body := strings.Join([]string{
		"intro text",
		"```lua :name hello :results output verbatim :session repl",
		"print(1)",
		"print(2)",
		"```",
		"between",
		"```python",
		"x = 1",
		"```",
	}, "\n")

	blocks := ParseBlocks(body)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d: %+v", len(blocks), blocks)
	}

	b0 := blocks[0]
	if b0.Language != "lua" || b0.Name != "hello" || b0.Ordinal != 0 {
		t.Fatalf("block0 meta: %+v", b0)
	}
	if b0.Body != "print(1)\nprint(2)" {
		t.Fatalf("block0 body: %q", b0.Body)
	}
	if b0.StartLine != 1 || b0.EndLine != 4 {
		t.Fatalf("block0 lines: start=%d end=%d", b0.StartLine, b0.EndLine)
	}
	wantArgs := map[string][]string{
		"name":    {"hello"},
		"results": {"output", "verbatim"},
		"session": {"repl"},
	}
	if !reflect.DeepEqual(b0.HeaderArgs, wantArgs) {
		t.Fatalf("block0 args: %+v", b0.HeaderArgs)
	}

	b1 := blocks[1]
	if b1.Language != "python" || b1.Name != "" || b1.Ordinal != 1 {
		t.Fatalf("block1 meta: %+v", b1)
	}
	if b1.HeaderArgs != nil {
		t.Fatalf("block1 should have no args, got %+v", b1.HeaderArgs)
	}
}

func TestParseBlocksSkipsPlainAndUnterminated(t *testing.T) {
	// A plain fence (no language) is consumed but not returned; an unterminated fence ends the scan.
	body := strings.Join([]string{
		"```",
		"plain text, no language",
		"```",
		"```ruby",
		"puts 1",
		"```",
		"```sh",
		"never closed",
	}, "\n")

	blocks := ParseBlocks(body)
	if len(blocks) != 1 {
		t.Fatalf("expected only the ruby block, got %+v", blocks)
	}
	if blocks[0].Language != "ruby" || blocks[0].Ordinal != 0 {
		t.Fatalf("unexpected block: %+v", blocks[0])
	}
}

func TestParseInfoStringVarAccumulates(t *testing.T) {
	_, args := parseInfoString("lua :var x=1 :var y=2 :eval no")
	if !reflect.DeepEqual(args["var"], []string{"x=1", "y=2"}) {
		t.Fatalf("var should accumulate, got %+v", args["var"])
	}
	if !reflect.DeepEqual(args["eval"], []string{"no"}) {
		t.Fatalf("eval: %+v", args["eval"])
	}
}

func TestBodyHashStableAndPrefixed(t *testing.T) {
	blocks := ParseBlocks("```lua\nprint(1)\n```")
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block")
	}
	if !strings.HasPrefix(blocks[0].BodyHash, "sha256:") {
		t.Fatalf("body hash should be prefixed: %q", blocks[0].BodyHash)
	}
	again := ParseBlocks("```lua\nprint(1)\n```")
	if again[0].BodyHash != blocks[0].BodyHash {
		t.Fatalf("body hash should be stable")
	}
}

func TestBlockID(t *testing.T) {
	named := Block{Name: "hello"}
	if named.ID(7) != "hello" {
		t.Fatalf("named id: %q", named.ID(7))
	}
	unnamed := Block{Language: "lua", Ordinal: 2, BodyHash: "sha256:deadbeefdeadbeefcafe"}
	if got := unnamed.ID(7); got != "7:2:lua:deadbeefdead" {
		t.Fatalf("unnamed id: %q", got)
	}
}

func TestValidateRejectsDuplicateNames(t *testing.T) {
	blocks := []Block{{Name: "a"}, {Name: "b"}, {Name: "a"}}
	if err := Validate(blocks); err == nil {
		t.Fatalf("expected duplicate-name error")
	}
	if err := Validate([]Block{{Name: "a"}, {Name: ""}, {Name: ""}, {Name: "b"}}); err != nil {
		t.Fatalf("unique names should validate, got %v", err)
	}
}
