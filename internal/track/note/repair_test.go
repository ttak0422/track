package note

import (
	"os"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func repairCfg(dir string) *config.Config {
	return &config.Config{VaultDir: dir, Extensions: []string{".md"}, DateFormat: "2006-01-02"}
}

// writeNote writes a note body for the given id and returns its path.
func writeNote(t *testing.T, c *config.Config, id int64, body string) string {
	t.Helper()
	path := c.NotePath(id)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	return path
}

func TestRepairRebuildsMissingSidecarFromBodyAndID(t *testing.T) {
	dir := t.TempDir()
	cfg := repairCfg(dir)
	// 1700000000 is a unix-seconds id => 2023-11-14 (local); assert on the recovered title only.
	path := writeNote(t, cfg, 1700000000, "# Recovered Title\n\nbody\n")

	res, err := RepairMetadata(path, cfg)
	if err != nil {
		t.Fatalf("RepairMetadata: %v", err)
	}
	if res.Status != RepairRebuilt {
		t.Fatalf("expected RepairRebuilt, got %v", res.Status)
	}
	if !res.TitleFound {
		t.Fatalf("expected title recovered from H1")
	}

	meta, found, err := ReadMetadata(cfg.MetadataPath(1700000000))
	if err != nil || !found {
		t.Fatalf("sidecar not written: found=%v err=%v", found, err)
	}
	if meta.Title != "Recovered Title" {
		t.Fatalf("title = %q, want %q", meta.Title, "Recovered Title")
	}
	if meta.Created == "" {
		t.Fatalf("expected created derived from id, got empty")
	}
	if meta.Version != CurrentMetadataVersion {
		t.Fatalf("version = %d, want %d", meta.Version, CurrentMetadataVersion)
	}
}

func TestRepairWithoutH1LeavesTitleEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := repairCfg(dir)
	path := writeNote(t, cfg, 1700000001, "no heading here\n")

	res, err := RepairMetadata(path, cfg)
	if err != nil {
		t.Fatalf("RepairMetadata: %v", err)
	}
	if res.Status != RepairRebuilt || res.TitleFound {
		t.Fatalf("expected rebuilt without title, got status=%v titleFound=%v", res.Status, res.TitleFound)
	}
	meta, found, err := ReadMetadata(cfg.MetadataPath(1700000001))
	if err != nil || !found {
		t.Fatalf("sidecar not written: found=%v err=%v", found, err)
	}
	if meta.Title != "" {
		t.Fatalf("title = %q, want empty", meta.Title)
	}
	if meta.Created == "" {
		t.Fatalf("expected created derived from id even without an H1")
	}
}

func TestRepairBackfillsCreatedOnValidSidecar(t *testing.T) {
	dir := t.TempDir()
	cfg := repairCfg(dir)
	id := int64(1700000002)
	writeNote(t, cfg, id, "# Title\n")
	// A valid sidecar carrying user-owned fields but missing created.
	if err := WriteMetadata(cfg.MetadataPath(id), Metadata{
		Version: CurrentMetadataVersion,
		Title:   "Title",
		Aliases: []string{"alt"},
		Tags:    []string{"keep"},
	}); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	res, err := RepairMetadata(cfg.NotePath(id), cfg)
	if err != nil {
		t.Fatalf("RepairMetadata: %v", err)
	}
	if res.Status != RepairBackfilled {
		t.Fatalf("expected RepairBackfilled, got %v", res.Status)
	}
	meta, _, err := ReadMetadata(cfg.MetadataPath(id))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if meta.Created == "" {
		t.Fatalf("created not backfilled")
	}
	// User-owned fields must survive untouched.
	if len(meta.Aliases) != 1 || meta.Aliases[0] != "alt" || len(meta.Tags) != 1 || meta.Tags[0] != "keep" {
		t.Fatalf("aliases/tags clobbered: %+v", meta)
	}
}

func TestRepairLeavesValidSidecarUntouched(t *testing.T) {
	dir := t.TempDir()
	cfg := repairCfg(dir)
	id := int64(1700000003)
	writeNote(t, cfg, id, "# Title\n")
	if err := WriteMetadata(cfg.MetadataPath(id), Metadata{
		Version: CurrentMetadataVersion,
		Title:   "Title",
		Created: "2023-01-01",
		Tags:    []string{"keep"},
	}); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	res, err := RepairMetadata(cfg.NotePath(id), cfg)
	if err != nil {
		t.Fatalf("RepairMetadata: %v", err)
	}
	if res.Status != RepairOK {
		t.Fatalf("expected RepairOK, got %v", res.Status)
	}
	meta, _, err := ReadMetadata(cfg.MetadataPath(id))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if meta.Created != "2023-01-01" {
		t.Fatalf("created changed to %q", meta.Created)
	}
}

func TestRepairRecoversFromLegacyFootmatter(t *testing.T) {
	dir := t.TempDir()
	cfg := repairCfg(dir)
	id := int64(1700000004)
	body := "# Legacy Note\n\nbody\n\n<!--track\nversion: 1\ntitle: Legacy Note\ntags:\n  - old\naliases:\n  - leg\ncreated: 2022-02-02\n-->\n"
	path := writeNote(t, cfg, id, body)

	res, err := RepairMetadata(path, cfg)
	if err != nil {
		t.Fatalf("RepairMetadata: %v", err)
	}
	if res.Status != RepairRecovered {
		t.Fatalf("expected RepairRecovered, got %v", res.Status)
	}
	meta, found, err := ReadMetadata(cfg.MetadataPath(id))
	if err != nil || !found {
		t.Fatalf("sidecar not written: found=%v err=%v", found, err)
	}
	// Legacy recovery is lossless: tags, aliases, and created come back.
	if meta.Created != "2022-02-02" || len(meta.Tags) != 1 || meta.Tags[0] != "old" || len(meta.Aliases) != 1 || meta.Aliases[0] != "leg" {
		t.Fatalf("legacy fields not recovered: %+v", meta)
	}
}

func TestCreatedFromIDByMagnitude(t *testing.T) {
	const f = "2006-01-02"
	if got := createdFromID(20260531, f); got != "2026-05-31" {
		t.Fatalf("journal id: got %q, want 2026-05-31", got)
	}
	if got := createdFromID(0, f); got != "" {
		t.Fatalf("zero id: got %q, want empty", got)
	}
	// Seconds and millisecond ids for the same instant resolve to the same date.
	sec := createdFromID(1700000000, f)
	milli := createdFromID(1700000000123, f)
	if sec == "" || sec != milli {
		t.Fatalf("seconds (%q) and millis (%q) should match and be non-empty", sec, milli)
	}
}
