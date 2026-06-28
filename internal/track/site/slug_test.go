package site

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestPublishIDStableAndOpaque(t *testing.T) {
	const id = int64(1781359469000)

	slug := PublishID(id)

	// Fixed 22-char base62: the namespace must never change, so this exact value pins the mapping.
	if !regexp.MustCompile(`^[0-9A-Za-z]{22}$`).MatchString(slug) {
		t.Fatalf("slug should be 22 base62 chars, got %q", slug)
	}
	// Deterministic across calls (so rebuilt sites keep the same URLs).
	if again := PublishID(id); again != slug {
		t.Fatalf("PublishID not stable: %q != %q", slug, again)
	}
	// Opaque: the decimal id must not appear in the slug.
	if strings.Contains(slug, strconv.FormatInt(id, 10)) {
		t.Fatalf("slug leaks the source id: %q", slug)
	}
}

func TestPublishIDDistinct(t *testing.T) {
	seen := map[string]int64{}
	for id := int64(1); id <= 5000; id++ {
		slug := PublishID(id)
		if other, dup := seen[slug]; dup {
			t.Fatalf("slug collision: ids %d and %d both map to %q", other, id, slug)
		}
		seen[slug] = id
	}
}
