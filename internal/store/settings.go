package store

import (
	"context"
	"database/sql"
	"errors"
)

// Setting reads one gateway setting; an unset key reads as "".
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM gateway_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// SetSetting writes one gateway setting; "" removes it.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if value == "" {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM gateway_settings WHERE key = $1`, key)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO gateway_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, value)
	return err
}
