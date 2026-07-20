package babel

import (
	"strings"
	"testing"
)

func parseOne(t *testing.T, body, name string) ([]Block, Block) {
	t.Helper()
	blocks := ParseBlocks(body)
	for _, b := range blocks {
		if b.Name == name {
			return blocks, b
		}
	}
	t.Fatalf("no block named %q in %q", name, body)
	return nil, Block{}
}

func TestExpandNowebRecursive(t *testing.T) {
	body := strings.Join([]string{
		"```sh :name inner",
		"echo inner",
		"```",
		"```sh :name middle",
		"<<inner>>",
		"echo middle",
		"```",
		"```sh :name outer :noweb yes",
		"<<middle>>",
		"echo outer",
		"```",
	}, "\n")
	blocks, outer := parseOne(t, body, "outer")

	got, err := ExpandNoweb(outer.Body, blocks)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	want := "echo inner\necho middle\necho outer"
	if got != want {
		t.Fatalf("expanded body:\n%q\nwant:\n%q", got, want)
	}
}

func TestExpandNowebKeepsIndentation(t *testing.T) {
	blocks := []Block{
		{Name: "body", Body: "line1\nline2"},
	}
	got, err := ExpandNoweb("if true; then\n  <<body>>\nfi", blocks)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	want := "if true; then\n  line1\n  line2\nfi"
	if got != want {
		t.Fatalf("indented expansion:\n%q\nwant:\n%q", got, want)
	}
}

func TestExpandNowebInlineSplice(t *testing.T) {
	blocks := []Block{{Name: "greet", Body: "hello"}}
	got, err := ExpandNoweb(`echo "<<greet>> world"`, blocks)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got != `echo "hello world"` {
		t.Fatalf("inline expansion: %q", got)
	}
}

func TestExpandNowebUnresolvedReference(t *testing.T) {
	_, err := ExpandNoweb("<<missing>>", nil)
	if err == nil || !strings.Contains(err.Error(), "<<missing>>") {
		t.Fatalf("want unresolved-reference error, got %v", err)
	}
}

func TestExpandNowebCycle(t *testing.T) {
	blocks := []Block{
		{Name: "a", Body: "<<b>>"},
		{Name: "b", Body: "<<a>>"},
	}
	_, err := ExpandNoweb("<<a>>", blocks)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want cycle error, got %v", err)
	}
	if !strings.Contains(err.Error(), "a -> b -> a") {
		t.Fatalf("cycle error should name the chain, got %v", err)
	}
}

func TestExpandNowebSelfCycle(t *testing.T) {
	blocks := []Block{{Name: "a", Body: "before\n<<a>>"}}
	if _, err := ExpandNoweb("<<a>>", blocks); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want self-cycle error, got %v", err)
	}
}

func TestNowebExpands(t *testing.T) {
	mk := func(v string) Block {
		if v == "" {
			return Block{}
		}
		return Block{HeaderArgs: map[string][]string{"noweb": {v}}}
	}
	cases := []struct {
		noweb       string
		eval, tangl bool
	}{
		{"", false, false},
		{"no", false, false},
		{"yes", true, true},
		{"eval", true, false},
		{"tangle", false, true},
	}
	for _, c := range cases {
		if got := NowebExpands(mk(c.noweb), "eval"); got != c.eval {
			t.Errorf(":noweb %q eval: got %v want %v", c.noweb, got, c.eval)
		}
		if got := NowebExpands(mk(c.noweb), "tangle"); got != c.tangl {
			t.Errorf(":noweb %q tangle: got %v want %v", c.noweb, got, c.tangl)
		}
	}
}
