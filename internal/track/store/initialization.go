package store

// Initialized reports whether a complete index build has finished. A missing
// marker is distinct from the empty link format used by legacy title links, and
// an empty vault can still have a completed build.
func (s *Store) Initialized() (bool, error) {
	var initialized bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM index_state WHERE key = 'link_format')`).Scan(&initialized)
	return initialized, err
}
