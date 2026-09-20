package store

import (
	"context"
	"database/sql"
	"errors"
)

// ErrNoLicense reports that this panel has never finished registering Evolution
// Go, which is not a failure — it is the state the activation form lives in.
var ErrNoLicense = errors.New("no evolution license registered")

// EvolutionLicense is the credential Evolution's licensing server issued for
// this deployment. Everything here is what activation needs again from
// scratch, which is the point of keeping it: Evolution can lose its own
// database volume and come back wanting a licence, and the operator should not
// have to repeat the registration to give it one.
type EvolutionLicense struct {
	OperatorEmail string
	InstanceID    string
	APIKey        string
	Tier          string
	CustomerID    int
}

// SaveOperatorEmail remembers which email a licence registration was started
// for. The operator typed it into the activation form; by the time the
// magic-link click comes back, there is no other copy of it anywhere the
// callback can reach.
func (s *Store) SaveOperatorEmail(ctx context.Context, email string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO evolution_license(singleton,operator_email) VALUES(TRUE,$1) ON CONFLICT(singleton) DO UPDATE SET operator_email=EXCLUDED.operator_email, updated_at=now()`, email)
	return err
}

// SaveEvolutionLicense records a completed registration. The email is kept
// from the earlier SaveOperatorEmail, since the activation callback does not
// carry it and there is no reason to make the operator's inbox part of the
// credential.
func (s *Store) SaveEvolutionLicense(ctx context.Context, license EvolutionLicense) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO evolution_license(singleton,operator_email,instance_id,api_key,customer_id,tier) VALUES(TRUE,'',$1,$2,$3,$4) ON CONFLICT(singleton) DO UPDATE SET instance_id=EXCLUDED.instance_id, api_key=EXCLUDED.api_key, customer_id=EXCLUDED.customer_id, tier=EXCLUDED.tier, updated_at=now()`, license.InstanceID, license.APIKey, license.CustomerID, license.Tier)
	return err
}

// EvolutionLicense returns the licence this deployment registered, if it ever
// did. A missing licence is reported as ErrNoLicense rather than an empty
// record, because the caller's next step is to offer the registration form.
func (s *Store) EvolutionLicense(ctx context.Context) (EvolutionLicense, error) {
	var license EvolutionLicense
	err := s.DB.QueryRowContext(ctx, `SELECT operator_email,instance_id,api_key,customer_id,tier FROM evolution_license WHERE singleton=TRUE`).Scan(&license.OperatorEmail, &license.InstanceID, &license.APIKey, &license.CustomerID, &license.Tier)
	if errors.Is(err, sql.ErrNoRows) {
		return EvolutionLicense{}, ErrNoLicense
	}
	return license, err
}
