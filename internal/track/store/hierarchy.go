package store

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ttak0422/track/internal/track/note"
)

// Hierarchy queries over the conventional "up" relation property (note.UpProp): a note declares its
// parent with "up:: [[Parent]]", and these resolve that declaration through the same keyword
// dictionary [[links]] use, so a parent is whatever the link would navigate to.

// UpNotes returns the notes a note's "up" properties point at, in property order. Values that do not
// resolve to a note are skipped, mirroring how an unresolved [[link]] is not a graph edge.
func (s *Store) UpNotes(id int64) ([]NoteRef, error) {
	rows, err := s.db.Query(
		`SELECT k.note_id, n.kind, n.title
		 FROM props p
		 JOIN keywords k ON k.term = p.value
		 JOIN notes n ON n.id = k.note_id
		 WHERE p.note_id = ? AND p.key = ? AND p.type = ? AND k.note_id != ?
		   AND n.kind IN ('note', 'journal')
		 GROUP BY k.note_id ORDER BY min(p.ord)`,
		id, note.UpProp, note.TypeLink, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNoteRefs(rows)
}

// ChildNotes returns the notes whose "up" property points at this note, in the shared note-list
// order (most recently updated first).
func (s *Store) ChildNotes(id int64) ([]NoteRef, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title
		 FROM props p
		 JOIN keywords k ON k.term = p.value AND k.note_id = ?
		 JOIN notes n ON n.id = p.note_id
		 WHERE p.key = ? AND p.type = ? AND n.id != ?
		   AND n.kind IN ('note', 'journal')
		 GROUP BY n.id ORDER BY n.mtime DESC, n.id`,
		id, note.UpProp, note.TypeLink, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNoteRefs(rows)
}

// HierarchyNode is one note in the vault-wide "up" tree: a reference plus the notes that name it as
// their parent.
type HierarchyNode struct {
	NoteRef
	Children []*HierarchyNode `json:"children,omitempty"`
}

// Hierarchy returns the whole vault's "up" tree, roots first and every level by title. This is the
// one listing that is not in the shared recently-updated-first order: a hierarchy is a structure
// someone laid out on purpose, and a file tree that reshuffles itself as its notes are edited cannot
// be navigated from memory. Only notes the hierarchy actually places are in it: a note with neither a
// parent nor a child never appears. A note with several "up" values follows the first, like Trail, so
// the result is a forest; a cycle keeps its members out of it entirely, since none of them is a root.
func (s *Store) Hierarchy() ([]*HierarchyNode, error) {
	// One row per child, carrying its first parent (min(p.ord) selects that row's parent columns) —
	// the parent is joined as a note too, so a root that declares no "up" of its own still arrives
	// with its title.
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title, pn.id, pn.kind, pn.title, min(p.ord)
		 FROM props p
		 JOIN keywords k ON k.term = p.value
		 JOIN notes n ON n.id = p.note_id
		 JOIN notes pn ON pn.id = k.note_id
		 WHERE p.key = ? AND p.type = ? AND pn.id != n.id
		   AND n.kind IN ('note', 'journal') AND pn.kind IN ('note', 'journal')
		 GROUP BY n.id`,
		note.UpProp, note.TypeLink,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := map[int64]*HierarchyNode{}
	child := map[int64]bool{}
	at := func(r NoteRef) *HierarchyNode {
		n, ok := nodes[r.NoteID]
		if !ok {
			n = &HierarchyNode{NoteRef: r}
			nodes[r.NoteID] = n
		}
		return n
	}
	for rows.Next() {
		var c, p NoteRef
		var ord int64
		if err := rows.Scan(&c.NoteID, &c.FileKind, &c.Title, &p.NoteID, &p.FileKind, &p.Title, &ord); err != nil {
			return nil, err
		}
		parent := at(p)
		parent.Children = append(parent.Children, at(c))
		child[c.NoteID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	roots := []*HierarchyNode{}
	for id, n := range nodes {
		slices.SortFunc(n.Children, compareHierarchyNodes)
		if !child[id] {
			roots = append(roots, n)
		}
	}
	slices.SortFunc(roots, compareHierarchyNodes)
	return roots, nil
}

// compareHierarchyNodes orders a level by title, case-insensitively (the comparison `SORT title`
// uses), with the id breaking ties so two same-titled notes keep a fixed order.
func compareHierarchyNodes(a, b *HierarchyNode) int {
	if c := cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
		return c
	}
	return cmp.Compare(a.NoteID, b.NoteID)
}

// Trail returns the chain of "up" ancestors of a note, root first, the immediate parent last. A note
// with several parents follows the first one (property order), so the trail is a single path; a
// cycle stops the walk where it would revisit a note.
func (s *Store) Trail(id int64) ([]NoteRef, error) {
	var trail []NoteRef
	seen := map[int64]bool{id: true}
	cur := id
	for {
		parents, err := s.UpNotes(cur)
		if err != nil {
			return nil, err
		}
		if len(parents) == 0 || seen[parents[0].NoteID] {
			return trail, nil
		}
		p := parents[0]
		seen[p.NoteID] = true
		trail = append([]NoteRef{p}, trail...)
		cur = p.NoteID
	}
}
