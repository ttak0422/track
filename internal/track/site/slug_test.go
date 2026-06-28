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

func TestPublishAssetName(t *testing.T) {
	const rel = "diagrams/My Secret Photo.PNG"

	name := publishAssetName(rel)

	// The extension is kept (lowercased) for media-kind/content-type detection; the rest is an opaque
	// 22-char slug that hides the source name and directory.
	if !regexp.MustCompile(`^[0-9A-Za-z]{22}\.png$`).MatchString(name) {
		t.Fatalf("asset name should be a 22-char slug plus lowercased ext, got %q", name)
	}
	if strings.Contains(name, "Secret") || strings.Contains(name, "diagrams") {
		t.Fatalf("asset name leaks the source path: %q", name)
	}
	if again := publishAssetName(rel); again != name {
		t.Fatalf("publishAssetName not stable: %q != %q", name, again)
	}
	// The "asset:" prefix keeps the asset and note-id slug spaces disjoint ("1" (no ext) vs id 1).
	if publishAssetName("1") == PublishID(1) {
		t.Fatalf("asset and note id spaces should not collide")
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
