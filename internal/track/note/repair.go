package note

import (
	"os"
	"strconv"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// RepairStatus classifies what RepairMetadata did to a single note's sidecar.
type RepairStatus int

const (
	// RepairOK means the sidecar was already valid and left untouched.
	RepairOK RepairStatus = iota
	// RepairBackfilled means a valid sidecar was missing only its created date, derived from the id.
	RepairBackfilled
	// RepairRecovered means a missing sidecar was rebuilt losslessly from a note's legacy footmatter.
	RepairRecovered
	// RepairRebuilt means a missing or unreadable sidecar was regenerated from the body and id alone.
	// Sidecar-only fields (aliases, tags, Babel block results) cannot be derived this way and are lost.
	RepairRebuilt
)

// RepairResult reports what happened to one note during a repair pass.
type RepairResult struct {
	ID         int64
	Path       string
	Status     RepairStatus
	TitleFound bool // the body had an H1, so the title was recoverable
	Corrupt    bool // a sidecar existed but was unreadable (bad YAML or unsupported version)
}

// RepairMetadata reconstructs the sidecar for the note at path as far as the body and id allow.
//
// The note body and id are authoritative for the fields they can express: the first H1 owns the
// title, and the id (a unix-time or yyyyMMdd value) owns the created date. A valid sidecar is left
// alone except for backfilling a missing created date. A missing or corrupt sidecar is regenerated;
// when a note still carries a legacy footmatter block the recovery is lossless, otherwise aliases,
// tags, and Babel block results—which only ever lived in the sidecar—cannot be recovered.
func RepairMetadata(path string, c *config.Config) (RepairResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return RepairResult{}, err
	}
	id, err := IDFromPath(path)
	if err != nil {
		return RepairResult{}, err
	}
	res := RepairResult{ID: id, Path: path}

	body, legacy, hasLegacy := SplitLegacyFootmatter(string(raw))
	metaPath := c.MetadataPath(id)
	meta, found, rerr := ReadMetadata(metaPath)
	res.Corrupt = found && rerr != nil

	if found && rerr == nil {
		// Valid sidecar: keep user-owned fields, only fill a missing created date.
		res.Status = RepairOK
		res.TitleFound = meta.Title != "" || FirstH1Title(body) != ""
		if meta.Created == "" {
			if created := createdFromID(id, c.DateFormat); created != "" {
				meta.Created = created
				if err := WriteMetadata(metaPath, meta); err != nil {
					return res, err
				}
				res.Status = RepairBackfilled
			}
		}
		return res, nil
	}

	// Missing or corrupt sidecar: prefer a lossless recovery from legacy footmatter, else rebuild.
	if hasLegacy {
		if lm, err := ParseLegacyMetadata(legacy); err == nil {
			meta = lm
			res.Status = RepairRecovered
		} else {
			meta = Metadata{}
			res.Status = RepairRebuilt
		}
	} else {
		meta = Metadata{}
		res.Status = RepairRebuilt
	}

	if meta.Version == 0 {
		meta.Version = CurrentMetadataVersion
	}
	if title := FirstH1Title(body); title != "" {
		meta.Title = title
		res.TitleFound = true
	}
	if meta.Created == "" {
		if created := createdFromID(id, c.DateFormat); created != "" {
			meta.Created = created
		}
	}
	if err := WriteMetadata(metaPath, meta); err != nil {
		return res, err
	}
	return res, nil
}

// createdFromID derives a created date from a note id. New notes use millisecond unix ids, early
// notes used second unix ids, and journals encode the date directly as yyyyMMdd; the magnitude of
// the id distinguishes the three.
func createdFromID(id int64, dateFormat string) string {
	switch {
	case id <= 0:
		return ""
	case id < 100_000_000: // 8-digit yyyyMMdd journal id, e.g. 20260531
		if t, err := time.Parse("20060102", strconv.FormatInt(id, 10)); err == nil {
			return t.Format(dateFormat)
		}
		return ""
	case id < 100_000_000_000: // unix seconds (~1973 to ~5138)
		return time.Unix(id, 0).Format(dateFormat)
	default: // unix milliseconds
		return time.UnixMilli(id).Format(dateFormat)
	}
}
