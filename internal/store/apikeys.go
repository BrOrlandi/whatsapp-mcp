package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// KeyPrefix marks every credential this gateway issues, so a leaked string is
// recognisable as one and can be searched for.
const KeyPrefix = "wamcp-"

// keyAlphabet excludes nothing: the key is copied, never typed from a reading,
// and 24 characters over 62 symbols give roughly 142 bits of entropy.
const keyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// ErrKeyUnknown reports a credential that does not match any live key. It never
// distinguishes "never existed" from "revoked": telling them apart would let a
// caller confirm which keys once existed.
var ErrKeyUnknown = errors.New("unknown or revoked API key")

// APIKey is one issued credential as the panel sees it. The secret itself
// exists only once, at creation.
type APIKey struct {
	ID         int64
	Name       string
	InstanceID string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt time.Time
	Revoked    bool
}

// NewAPIKey mints a credential and returns both the secret, shown once, and the
// digest stored in its place.
func NewAPIKey() (secret, digest, prefix string, err error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", err
	}
	var builder strings.Builder
	builder.WriteString(KeyPrefix)
	for _, b := range raw {
		builder.WriteByte(keyAlphabet[int(b)%len(keyAlphabet)])
	}
	secret = builder.String()
	return secret, HashAPIKey(secret), secret[:len(KeyPrefix)+6], nil
}

// HashAPIKey digests a credential for storage and lookup.
//
// SHA-256 rather than bcrypt: bcrypt's deliberate cost is what protects a
// human-chosen password against a dictionary, and it is the wrong tool for a
// high-entropy key verified on every MCP request, where it would add around
// 100 ms per call and guard against an attack that cannot succeed anyway.
func HashAPIKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// CreateAPIKey stores a new credential for one instance.
func (s *Store) CreateAPIKey(ctx context.Context, name, instanceID, digest, prefix string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO api_keys(name,instance_id,key_hash,key_prefix) VALUES($1,$2,$3,$4)`, name, instanceID, digest, prefix)
	return err
}

// ResolveAPIKey authenticates a presented credential and returns the instance
// it is bound to. The digest comparison is constant time, and the lookup itself
// is by digest so the secret never reaches a query log.
func (s *Store) ResolveAPIKey(ctx context.Context, secret string) (APIKey, error) {
	if !strings.HasPrefix(secret, KeyPrefix) {
		return APIKey{}, ErrKeyUnknown
	}
	digest := HashAPIKey(secret)
	var key APIKey
	var stored string
	var lastUsed sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,instance_id,key_prefix,key_hash,created_at,last_used_at FROM api_keys WHERE key_hash=$1 AND revoked_at IS NULL`, digest).
		Scan(&key.ID, &key.Name, &key.InstanceID, &key.Prefix, &stored, &key.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrKeyUnknown
	}
	if err != nil {
		return APIKey{}, err
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(digest)) != 1 {
		return APIKey{}, ErrKeyUnknown
	}
	if lastUsed.Valid {
		key.LastUsedAt = lastUsed.Time.UTC()
	}
	return key, nil
}

// TouchAPIKey records that a credential was used, which is what lets the panel
// show a key nobody uses any more.
func (s *Store) TouchAPIKey(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, id)
	return err
}

// ListAPIKeys returns the live credentials, newest first.
func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,instance_id,key_prefix,created_at,last_used_at FROM api_keys WHERE revoked_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []APIKey
	for rows.Next() {
		var key APIKey
		var lastUsed sql.NullTime
		if err := rows.Scan(&key.ID, &key.Name, &key.InstanceID, &key.Prefix, &key.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			key.LastUsedAt = lastUsed.Time.UTC()
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// RevokeAPIKey takes a credential out of service immediately.
func (s *Store) RevokeAPIKey(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	return err
}
