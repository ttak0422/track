package export

import (
	"github.com/ttak0422/track/internal/track/babel"
	"strings"
	"testing"
)

func TestBabelResultsFenceAndSuppression(t *testing.T) {
	result := &babel.RunResult{Stdout: "before\n```\n````\nafter\n"}
	got := renderResults(babel.Block{}, result)
	if !strings.HasPrefix(got, "`````\n") || !strings.HasSuffix(got, "\n`````") {
		t.Fatalf("unsafe fence: %q", got)
	}
	for _, mode := range []string{"none", "discard"} {
		if got := renderResults(babel.Block{HeaderArgs: map[string][]string{"results": {mode}}}, result); got != "" {
			t.Fatalf("%s displayed %q", mode, got)
		}
	}
}
