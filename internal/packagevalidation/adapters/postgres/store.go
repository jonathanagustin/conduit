// Package postgres persists validation evidence and challenge consumption.
// It supplies no producer, HTTP endpoint, approval or deployment authority.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/application"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/ports"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

const Component = "package-validation"
const maximumStateBytes = 1 << 20

var ErrMissing = errors.New("validation attempt not found")
var ErrUsed = errors.New("validation challenge already consumed")

// Authorize must authenticate the original caller and lock current trusted
// owner/target/policy facts through tx until commit. It must not open another
// transaction or re-enter Store. This required integration port is not a default
// permit: an uncomposed store must not be exposed to receipt producers.
type Authorize func(context.Context, *sql.Tx, ports.Locator) error

type Store struct {
	db        *sql.DB
	authorize Authorize
	now       func() time.Time
}

var migrations = []pgmigrate.Migration{{Name: "0001-validation-attempt-admission-audit", Statements: `
CREATE TABLE conduit_validation_attempts (
 run_id text NOT NULL CHECK (octet_length(run_id) BETWEEN 1 AND 256),
 proposal_revision text NOT NULL CHECK (octet_length(proposal_revision) BETWEEN 1 AND 256),
 challenge text NOT NULL UNIQUE CHECK (challenge ~ '^[a-f0-9]{64}$'),
 payload bytea NOT NULL CHECK (octet_length(payload) BETWEEN 2 AND 1048576),
 consumed boolean NOT NULL DEFAULT false,
 PRIMARY KEY(run_id,proposal_revision)
);
CREATE TABLE conduit_validation_admissions (
 run_id text NOT NULL,
 proposal_revision text NOT NULL,
 receipt_id text NOT NULL UNIQUE,
 payload bytea NOT NULL CHECK (octet_length(payload) BETWEEN 2 AND 1048576),
 PRIMARY KEY(run_id,proposal_revision),
 FOREIGN KEY(run_id,proposal_revision) REFERENCES conduit_validation_attempts(run_id,proposal_revision)
);
CREATE TABLE conduit_validation_admission_audit (
 receipt_id text PRIMARY KEY REFERENCES conduit_validation_admissions(receipt_id),
 run_id text NOT NULL,
 proposal_revision text NOT NULL,
 payload bytea NOT NULL CHECK (octet_length(payload) BETWEEN 2 AND 1048576),
 FOREIGN KEY(run_id,proposal_revision) REFERENCES conduit_validation_attempts(run_id,proposal_revision)
);`}}

func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }
func New(ctx context.Context, dsn string, authorize Authorize) (*Store, error) {
	if authorize == nil {
		return nil, domain.ErrInvalid
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	var fsync string
	if err := db.QueryRowContext(ctx, "SHOW fsync").Scan(&fsync); err != nil || fsync != "on" {
		_ = db.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("validation authority requires PostgreSQL fsync")
	}
	if err = pgmigrate.Apply(ctx, db, Component, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, authorize: authorize, now: time.Now}, nil
}
func (s *Store) Close() error { return s.db.Close() }

// transaction serializes one exact locator even before its row exists. The
// mandatory authorizer runs before attempt lookup and holds its locks through
// this transaction. Hash collisions only serialize unrelated locators.
func (s *Store) transaction(ctx context.Context, loc ports.Locator, work func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scope, err := json.Marshal(loc)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL synchronous_commit = on`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, Component+":"+string(scope)); err != nil {
		return err
	}
	if err = s.authorize(ctx, tx, loc); err != nil {
		return err
	}
	if err = work(tx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}
func encode(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) > maximumStateBytes {
		return nil, domain.ErrInvalid
	}
	return b, nil
}
func readAttempt(ctx context.Context, tx *sql.Tx, loc ports.Locator) (ports.Attempt, error) {
	var raw []byte
	var consumed bool
	var challenge string
	err := tx.QueryRowContext(ctx, `SELECT payload,consumed,challenge FROM conduit_validation_attempts WHERE run_id=$1 AND proposal_revision=$2 FOR UPDATE`, loc.RunID, loc.ProposalRevision).Scan(&raw, &consumed, &challenge)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Attempt{}, ErrMissing
	}
	if err != nil {
		return ports.Attempt{}, err
	}
	var a ports.Attempt
	if json.Unmarshal(raw, &a) != nil || application.ValidateAttempt(loc, a) != nil || a.Consumed != consumed || a.Manifest.Challenge != challenge {
		return ports.Attempt{}, application.ErrCorrupt
	}
	return a, nil
}

// SetAttempt accepts trusted owner facts only, never generated receipt fields.
// Existing locators/challenges cannot be reset, including across restarts.
func (s *Store) SetAttempt(ctx context.Context, loc ports.Locator, a ports.Attempt) error {
	if application.ValidateAttempt(loc, a) != nil || a.Consumed {
		return domain.ErrInvalid
	}
	raw, err := encode(a)
	if err != nil {
		return err
	}
	return s.transaction(ctx, loc, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO conduit_validation_attempts(run_id,proposal_revision,challenge,payload) VALUES($1,$2,$3,$4)`, loc.RunID, loc.ProposalRevision, a.Manifest.Challenge, raw)
		return err
	})
}

// Update is trusted owner control. A new challenge or consumed attempt needs a
// new revision; cancellation cannot be undone. No public setup API is supplied.
func (s *Store) Update(ctx context.Context, loc ports.Locator, change func(*ports.Attempt) error) error {
	if change == nil {
		return domain.ErrInvalid
	}
	return s.transaction(ctx, loc, func(tx *sql.Tx) error {
		old, err := readAttempt(ctx, tx, loc)
		if err != nil {
			return err
		}
		if old.Consumed {
			return ErrUsed
		}
		raw, err := encode(old)
		if err != nil {
			return err
		}
		var next ports.Attempt
		if err = json.Unmarshal(raw, &next); err != nil {
			return err
		}
		if err = change(&next); err != nil {
			return err
		}
		if next.Manifest.Challenge != old.Manifest.Challenge || next.Consumed != old.Consumed || (old.Cancelled && !next.Cancelled) {
			return domain.ErrUntrusted
		}
		if err = application.ValidateAttempt(loc, next); err != nil {
			return err
		}
		raw, err = encode(next)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE conduit_validation_attempts SET payload=$3 WHERE run_id=$1 AND proposal_revision=$2`, loc.RunID, loc.ProposalRevision, raw)
		return err
	})
}
func (s *Store) Cancel(ctx context.Context, loc ports.Locator) error {
	return s.Update(ctx, loc, func(a *ports.Attempt) error { a.Cancelled = true; return nil })
}

func (s *Store) UseAttempt(ctx context.Context, loc ports.Locator, verify func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error) {
	if verify == nil {
		return ports.Admission{}, domain.ErrInvalid
	}
	var result ports.Admission
	err := s.transaction(ctx, loc, func(tx *sql.Tx) error {
		attempt, err := readAttempt(ctx, tx, loc)
		if err != nil {
			return err
		}
		if attempt.Consumed {
			return ErrUsed
		}
		if attempt.Cancelled {
			return domain.ErrUntrusted
		}
		// Isolate trusted facts from a verifier that mutates its callback argument.
		raw, err := encode(attempt)
		if err != nil {
			return err
		}
		var view ports.Attempt
		if err = json.Unmarshal(raw, &view); err != nil {
			return err
		}
		record, err := verify(view)
		if err != nil {
			return err
		}
		if err = application.VerifyAdmission(attempt, record, s.now().UTC()); err != nil {
			return err
		}
		payload, err := encode(record)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO conduit_validation_admissions(run_id,proposal_revision,receipt_id,payload) VALUES($1,$2,$3,$4)`, loc.RunID, loc.ProposalRevision, record.ID, payload); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO conduit_validation_admission_audit(receipt_id,run_id,proposal_revision,payload) VALUES($1,$2,$3,$4)`, record.ID, loc.RunID, loc.ProposalRevision, payload); err != nil {
			return err
		}
		attempt.Consumed = true
		raw, err = encode(attempt)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE conduit_validation_attempts SET consumed=true,payload=$3 WHERE run_id=$1 AND proposal_revision=$2`, loc.RunID, loc.ProposalRevision, raw); err != nil {
			return err
		}
		attempt.Consumed = false // verify original trusted facts immediately before commit.
		if err = application.VerifyAdmission(attempt, record, s.now().UTC()); err != nil {
			return err
		}
		// Decode the persisted bytes; returned maps/bytes must not alias callback data.
		return json.Unmarshal(payload, &result)
	})
	if err != nil {
		return ports.Admission{}, err
	}
	if err = ctx.Err(); err != nil {
		return ports.Admission{}, err
	}
	now := s.now().UTC()
	if now.Before(result.VerifiedAt) || !now.Before(result.ExpiresAt) {
		return ports.Admission{}, domain.ErrUntrusted
	}
	return result, nil
}

// GetAdmission returns authorized historical evidence, not a fresh verification
// or permission to deploy. Final handoff must revalidate current facts/expiry.
func (s *Store) GetAdmission(ctx context.Context, loc ports.Locator) (ports.Admission, error) {
	var result ports.Admission
	err := s.transaction(ctx, loc, func(tx *sql.Tx) error {
		var raw []byte
		err := tx.QueryRowContext(ctx, `SELECT payload FROM conduit_validation_admissions WHERE run_id=$1 AND proposal_revision=$2`, loc.RunID, loc.ProposalRevision).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMissing
		}
		if err != nil {
			return err
		}
		if json.Unmarshal(raw, &result) != nil || result.Manifest.RunID != loc.RunID || result.Manifest.ProposalRevision != loc.ProposalRevision {
			return application.ErrCorrupt
		}
		return nil
	})
	if err != nil {
		return ports.Admission{}, err
	}
	return result, nil
}

var _ ports.Authority = (*Store)(nil)
