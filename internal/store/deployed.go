package store

import (
	"context"
	"database/sql"
	"errors"
)

// HighestVersion returns the newest release that has run against this
// database, or "" before the first.
func (s *Store) HighestVersion(ctx context.Context) (string, error) {
	var highest string
	err := s.DB.QueryRowContext(ctx, `SELECT highest FROM deployed_version WHERE singleton=TRUE`).Scan(&highest)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return highest, err
}

// SaveHighestVersion records a new high-water mark. The caller decides that
// it is higher; this only writes it.
func (s *Store) SaveHighestVersion(ctx context.Context, v string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO deployed_version(singleton,highest) VALUES(TRUE,$1) ON CONFLICT(singleton) DO UPDATE SET highest=EXCLUDED.highest, updated_at=now()`, v)
	return err
}
