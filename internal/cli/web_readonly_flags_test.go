package cli

import "testing"

func TestWebCanonicalReadOnlyFlagsRequirePair(t *testing.T) {
	for _, args := range [][]string{{"--canonical-read-only"}, {"--read-only-cache-root", "/unused/private"}, {"--canonical-read-only=false", "--read-only-cache-root", "/unused/private"}} {
		if code := cmdWeb(args); code == 0 {
			t.Fatalf("unpaired flags accepted: %v", args)
		}
	}
}
