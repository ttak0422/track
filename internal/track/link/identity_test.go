package link

import "testing"

func TestExplicitIdentityIsOptInAndNeverNumericGuess(t *testing.T) {
	for _, tt := range []struct {
		key, format string
		id          int64
		explicit    bool
	}{
		{"123", IDFormat, 0, false}, {"id:123", "", 0, false},
		{"id:123", "title", 0, false}, {"id:123", IDFormat, 123, true},
		{"id:", IDFormat, 0, true}, {"id:001", IDFormat, 0, true},
		{"id:-1", IDFormat, 0, true}, {"id:1x", IDFormat, 0, true},
		{"id:9223372036854775808", IDFormat, 0, true},
	} {
		id, explicit := ExplicitID(tt.key, tt.format)
		if id != tt.id || explicit != tt.explicit {
			t.Errorf("ExplicitID(%q,%q) = %d,%v", tt.key, tt.format, id, explicit)
		}
	}
}

func TestIdentityTargetsComposeWithAliasesAnchorsIncludes(t *testing.T) {
	for _, tt := range []struct {
		anchor         string
		level          int
		heading, block string
	}{
		{"", 0, "", ""}, {"##Heading", 2, "Heading", ""}, {"#^block", 0, "", "block"},
	} {
		target := AnchoredNoteTarget(IDFormat, 123, "日本語 title", tt.anchor)
		for _, prefix := range []string{"", "work:"} {
			refs := Refs("![[" + prefix + target + "]]")
			if len(refs) != 1 || refs[0].Text != prefix+"id:123" || refs[0].Display != "日本語 title" || refs[0].HeadingLevel != tt.level || refs[0].Heading != tt.heading || refs[0].BlockID != tt.block {
				t.Fatalf("refs: %+v", refs)
			}
			if inc := Includes("![[" + prefix + target + "]]"); len(inc) != 1 || inc[0].Text != prefix+"id:123" {
				t.Fatalf("includes: %+v", inc)
			}
		}
	}
	for _, unsafe := range []string{"bad|alias", "bad]]", "line\nbreak", "unbalanced["} {
		if got := NoteTarget(IDFormat, 123, unsafe); got != "id:123" {
			t.Errorf("unsafe target %q", got)
		}
	}
	if got := NoteTarget("", 123, "old title"); got != "old title" {
		t.Fatal(got)
	}
}
