package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

var ErrOAuthGrant = errors.New("invalid OAuth grant")

type OAuthAuthorization struct {
	ClientID, RedirectURI, Resource, Challenge, InstanceID string
}

type OAuthExchange struct {
	GrantType, Credential, ClientID, RedirectURI, Resource, Challenge string
}

type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func oauthSecret(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// CreateOAuthCode binds approval to one instance and leaves its key unusable
// until a client proves possession of the PKCE verifier.
func (s *Store) CreateOAuthCode(ctx context.Context, grant OAuthAuthorization) (string, error) {
	code, err := oauthSecret("wa-code-")
	if err != nil {
		return "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var keyID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO api_keys(name,instance_id,key_hash,key_prefix,client_name,expires_at)
		VALUES($1,$2,$3,'OAuth',$4,now()) RETURNING id`, "OAuth: "+grant.ClientID, grant.InstanceID,
		HashAPIKey("unissued:"+code), grant.ClientID).Scan(&keyID)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_grants
		(key_id,client_id,redirect_uri,resource,challenge,code_hash,code_expires_at,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,now()+interval '5 minutes',now()+interval '30 days')`,
		keyID, grant.ClientID, grant.RedirectURI, grant.Resource, grant.Challenge, HashAPIKey(code))
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return code, nil
}

// ExchangeOAuthToken consumes codes and rotates refresh tokens in a single
// transaction. Row locks make concurrent exchanges obey the same one-use rule.
func (s *Store) ExchangeOAuthToken(ctx context.Context, input OAuthExchange) (OAuthToken, error) {
	access, digest, prefix, err := NewAPIKey()
	if err != nil {
		return OAuthToken{}, err
	}
	refresh, err := oauthSecret("wa-refresh-")
	if err != nil {
		return OAuthToken{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return OAuthToken{}, err
	}
	defer tx.Rollback()
	var keyID int64
	var grantExpiry time.Time
	switch input.GrantType {
	case "authorization_code":
		err = tx.QueryRowContext(ctx, `SELECT key_id,expires_at FROM oauth_grants
			WHERE code_hash=$1 AND client_id=$2 AND redirect_uri=$3 AND resource=$4 AND challenge=$5
			AND code_expires_at>now() AND expires_at>now() FOR UPDATE`, HashAPIKey(input.Credential),
			input.ClientID, input.RedirectURI, input.Resource, input.Challenge).Scan(&keyID, &grantExpiry)
	case "refresh_token":
		var used sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT g.key_id,g.expires_at,t.used_at
			FROM oauth_refresh_tokens t JOIN oauth_grants g ON g.key_id=t.key_id
			WHERE t.token_hash=$1 AND g.client_id=$2 AND g.resource=$3 AND g.expires_at>now()
			FOR UPDATE OF g,t`, HashAPIKey(input.Credential), input.ClientID, input.Resource).
			Scan(&keyID, &grantExpiry, &used)
		if err == nil && used.Valid {
			if _, err = tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=now() WHERE id=$1`, keyID); err != nil {
				return OAuthToken{}, err
			}
			if err = tx.Commit(); err != nil {
				return OAuthToken{}, err
			}
			return OAuthToken{}, ErrOAuthGrant
		}
	default:
		return OAuthToken{}, ErrOAuthGrant
	}
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthToken{}, ErrOAuthGrant
	}
	if err != nil {
		return OAuthToken{}, err
	}
	var expiry time.Time
	err = tx.QueryRowContext(ctx, `UPDATE api_keys SET key_hash=$2,key_prefix=$3,
		expires_at=LEAST(now()+interval '1 hour',$4) WHERE id=$1 AND revoked_at IS NULL RETURNING expires_at`,
		keyID, digest, prefix, grantExpiry).Scan(&expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthToken{}, ErrOAuthGrant
	}
	if err != nil {
		return OAuthToken{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_grants SET code_hash=NULL WHERE key_id=$1`, keyID); err != nil {
		return OAuthToken{}, err
	}
	if input.GrantType == "refresh_token" {
		if _, err = tx.ExecContext(ctx, `UPDATE oauth_refresh_tokens SET used_at=now() WHERE token_hash=$1`, HashAPIKey(input.Credential)); err != nil {
			return OAuthToken{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO oauth_refresh_tokens(token_hash,key_id) VALUES($1,$2)`, HashAPIKey(refresh), keyID); err != nil {
		return OAuthToken{}, err
	}
	if err = tx.Commit(); err != nil {
		return OAuthToken{}, err
	}
	return OAuthToken{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(time.Until(expiry).Seconds()),
		RefreshToken: refresh, Scope: "whatsapp"}, nil
}

// RevokeOAuthToken is deliberately indistinguishable for unknown credentials.
func (s *Store) RevokeOAuthToken(ctx context.Context, clientID, secret string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE api_keys k SET revoked_at=now() FROM oauth_grants g
		WHERE g.key_id=k.id AND g.client_id=$1 AND
		(k.key_hash=$2 OR EXISTS(SELECT 1 FROM oauth_refresh_tokens t WHERE t.key_id=k.id AND t.token_hash=$2))`,
		clientID, HashAPIKey(secret))
	return err
}
