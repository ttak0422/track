package store

// Revision changes on every cache publication. It rejects an old staging pass
// after another process indexed a mutation, without a long filesystem lock.
func (s *Store) Revision() (int64, error) {
	var revision int64
	err := s.runner().QueryRow(`SELECT CAST(value AS INTEGER) FROM index_state WHERE key='snapshot_revision'`).Scan(&revision)
	if isNoRows(err) {
		return 0, nil
	}
	return revision, err
}
func (s *Store) InvalidateSnapshot() error {
	_, err := s.runner().Exec(`INSERT INTO index_state(key,value) VALUES('snapshot_revision','1') ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1`)
	return err
}
