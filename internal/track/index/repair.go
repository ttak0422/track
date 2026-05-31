package index

import "github.com/ttak0422/track/internal/track/note"

// RepairReport summarizes a metadata repair pass over every note in the vault.
type RepairReport struct {
	Scanned      int // notes inspected
	OK           int // valid sidecars left untouched
	Backfilled   int // valid sidecars whose missing created date was derived from the id
	Recovered    int // missing sidecars rebuilt losslessly from legacy footmatter
	Rebuilt      int // missing/unreadable sidecars regenerated from body+id (aliases/tags/blocks lost)
	Corrupt      int // sidecars that existed but were unreadable (subset of Rebuilt)
	MissingTitle int // rebuilt notes with no H1 to recover a title from (subset of Rebuilt)
}

// Repair walks every note and reconstructs missing or unreadable sidecars as far as each note's
// body and id allow. It does not touch the index; callers should reindex afterwards so search and
// links reflect the rebuilt sidecars.
func (ix *Indexer) Repair() (RepairReport, error) {
	paths, err := ix.scanFiles()
	if err != nil {
		return RepairReport{}, err
	}

	var rep RepairReport
	for _, p := range paths {
		res, err := note.RepairMetadata(p, ix.cfg)
		if err != nil {
			return rep, err
		}
		rep.Scanned++
		switch res.Status {
		case note.RepairOK:
			rep.OK++
		case note.RepairBackfilled:
			rep.Backfilled++
		case note.RepairRecovered:
			rep.Recovered++
		case note.RepairRebuilt:
			rep.Rebuilt++
			if !res.TitleFound {
				rep.MissingTitle++
			}
		}
		if res.Corrupt {
			rep.Corrupt++
		}
	}
	return rep, nil
}
