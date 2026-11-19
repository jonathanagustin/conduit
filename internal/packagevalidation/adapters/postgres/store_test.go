package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/application"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/ports"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

type ownerKey struct{}
type readOnlyKey struct{}

var errDenied = errors.New("fixture caller denied")

func digest(c string) string { return "sha256:" + strings.Repeat(c, 64) }
func signedFixture(t *testing.T) (ports.Locator, ports.Attempt, []byte) {
	t.Helper()
	now := time.Now().UTC()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tool := domain.Tool{Name: "synthetic", Version: "test-v1", ExecutableDigest: digest("a")}
	policy := domain.Policy{Enabled: true, ProfileDigest: digest("b"), MaximumAge: 15 * time.Minute, Checks: map[string]domain.CheckPolicy{"check": {Tool: tool, InputDigest: digest("c")}}, Producers: map[string]domain.Producer{domain.ProducerID(public): {PublicKey: public, Profiles: []string{digest("b")}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}}
	pin, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifest := domain.Manifest{RunID: "run", ProposalRevision: "proposal", SourceDigest: digest("a"), TemplateDigest: digest("b"), ProfileDigest: digest("b"), PolicyDigest: pin, ArtifactDigest: digest("c"), ToolchainDigest: digest("d"), TargetDigest: digest("e"), ConfigurationDigest: digest("f"), Challenge: strings.Repeat("a", 64)}
	claim := domain.Attestation{Version: domain.Version, ProducerID: domain.ProducerID(public), Manifest: manifest, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Checks: []domain.Check{{ID: "check", Status: "PASS", Tool: tool, InputDigest: digest("c"), OutputDigest: digest("d"), DiagnosticsDigest: digest("e"), StartedAt: now.Add(-2 * time.Minute), FinishedAt: now.Add(-time.Minute)}}}
	payload, err := claim.SigningPayload()
	if err != nil {
		t.Fatal(err)
	}
	claim.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, payload))
	raw, err := domain.Encode(claim)
	if err != nil {
		t.Fatal(err)
	}
	return ports.Locator{RunID: manifest.RunID, ProposalRevision: manifest.ProposalRevision}, ports.Attempt{Manifest: manifest, Policy: policy}, raw
}
func freshDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dsn == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set; actual PostgreSQL qualification not run")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fresh, drop, err := pgtest.Fresh(t.Context(), db, dsn, "validation")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return fresh
}
func authorizeOwner(ctx context.Context, tx *sql.Tx, action Action, loc ports.Locator) error {
	if ctx.Value(readOnlyKey{}) == true && action != ActionReadAdmission {
		return errDenied
	}
	var owner, commit string
	if err := tx.QueryRowContext(ctx, `SELECT owner FROM validation_fixture_owners WHERE run_id=$1 AND revision=$2 FOR SHARE`, loc.RunID, loc.ProposalRevision).Scan(&owner); err != nil {
		return errDenied
	}
	if ctx.Value(ownerKey{}) != owner {
		return errDenied
	}
	if err := tx.QueryRowContext(ctx, `SHOW synchronous_commit`).Scan(&commit); err != nil {
		return err
	}
	if commit != "on" {
		return errors.New("commit durability disabled")
	}
	return nil
}
func fixture(t *testing.T) (*Store, *application.Service, string, context.Context, ports.Locator, ports.Attempt, []byte) {
	t.Helper()
	loc, attempt, raw := signedFixture(t)
	dsn := freshDSN(t)
	s, err := New(t.Context(), dsn, authorizeOwner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err = s.db.ExecContext(t.Context(), `CREATE TABLE validation_fixture_owners(run_id text,revision text,owner text,PRIMARY KEY(run_id,revision)); INSERT INTO validation_fixture_owners VALUES('run','proposal','owner')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), ownerKey{}, "owner")
	if err = s.SetAttempt(ctx, loc, attempt); err != nil {
		t.Fatal(err)
	}
	service, err := application.New(s)
	if err != nil {
		t.Fatal(err)
	}
	return s, service, dsn, ctx, loc, attempt, raw
}
func counts(t *testing.T, s *Store, loc ports.Locator, want int) {
	t.Helper()
	var records, audits int
	var used bool
	if err := s.db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM conduit_validation_admissions),(SELECT count(*) FROM conduit_validation_admission_audit),consumed FROM conduit_validation_attempts WHERE run_id=$1 AND proposal_revision=$2`, loc.RunID, loc.ProposalRevision).Scan(&records, &audits, &used); err != nil {
		t.Fatal(err)
	}
	if records != want || audits != want || used != (want == 1) {
		t.Fatalf("torn admission/audit/consumption: %d %d %t want%d", records, audits, used, want)
	}
}

func TestNewRequiresAuthorizerBeforeOpeningDatabase(t *testing.T) {
	if _, err := New(t.Context(), "not a DSN", nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if err := pgmigrate.Validate(Component, Migrations()); err != nil {
		t.Fatal(err)
	}
	copy := Migrations()
	copy[0].Name = "changed"
	if Migrations()[0].Name != "0001-validation-attempt-admission-audit" {
		t.Fatal("mutable migration list")
	}
}

func TestPostgresAdmissionContendsAcrossPoolsAndSurvivesRestart(t *testing.T) {
	first, service, dsn, ctx, loc, _, raw := fixture(t)
	second, err := New(t.Context(), dsn, authorizeOwner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	other, err := application.New(second)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			api := service
			if i%2 == 1 {
				api = other
			}
			if _, err := api.Admit(ctx, loc, raw); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrUsed) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("challenge used %d times", successes.Load())
	}
	counts(t, first, loc, 1)
	record, err := first.GetAdmission(ctx, loc)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(t.Context(), dsn, authorizeOwner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := reopened.GetAdmission(ctx, loc)
	if err != nil || got.ID != record.ID || string(got.Attestation) != string(record.Attestation) {
		t.Fatalf("restart lost exact evidence: %v", err)
	}
	api, _ := application.New(reopened)
	if _, err = api.Admit(ctx, loc, raw); !errors.Is(err, ErrUsed) {
		t.Fatalf("restart replay=%v", err)
	}
	counts(t, reopened, loc, 1)
	got.Attestation[0] = '!'
	again, err := reopened.GetAdmission(ctx, loc)
	if err != nil || string(again.Attestation) != string(record.Attestation) {
		t.Fatal("mutable returned receipt", err)
	}
}

func TestPostgresAdmissionRollsBackEveryWriteBoundary(t *testing.T) {
	for _, table := range []string{"conduit_validation_admissions", "conduit_validation_admission_audit", "conduit_validation_attempts"} {
		t.Run(table, func(t *testing.T) {
			s, service, _, ctx, loc, _, raw := fixture(t)
			if _, err := s.db.ExecContext(t.Context(), `CREATE FUNCTION reject_validation_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected validation failure'; END $$`); err != nil {
				t.Fatal(err)
			}
			// Fixed table literals above; these objects belong to pgtest.Fresh only.
			if _, err := s.db.ExecContext(t.Context(), `CREATE TRIGGER reject_validation BEFORE INSERT OR UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION reject_validation_write()`); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Admit(ctx, loc, raw); err == nil {
				t.Fatal("failed write acknowledged")
			}
			counts(t, s, loc, 0)
			if _, err := s.GetAdmission(ctx, loc); !errors.Is(err, ErrMissing) {
				t.Fatalf("partial record=%v", err)
			}
			if _, err := s.db.ExecContext(t.Context(), `DROP TRIGGER reject_validation ON `+table); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Admit(ctx, loc, raw); err != nil {
				t.Fatal(err)
			}
			counts(t, s, loc, 1)
		})
	}
}

func TestPostgresTrustedFactsCancellationAndChallengeCannotBeReplayed(t *testing.T) {
	s, service, _, ctx, loc, attempt, raw := fixture(t)
	if err := s.SetAttempt(ctx, loc, attempt); err == nil {
		t.Fatal("locator reset")
	}
	if err := s.Update(ctx, loc, func(a *ports.Attempt) error { a.Manifest.Challenge = strings.Repeat("b", 64); return nil }); !errors.Is(err, domain.ErrUntrusted) {
		t.Fatalf("challenge reset=%v", err)
	}
	if err := s.Update(ctx, loc, func(a *ports.Attempt) error { a.Manifest.ConfigurationDigest = digest("a"); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Admit(ctx, loc, raw); !errors.Is(err, domain.ErrUntrusted) {
		t.Fatalf("stale content admitted=%v", err)
	}
	counts(t, s, loc, 0)
	if err := s.Update(ctx, loc, func(a *ports.Attempt) error { a.Manifest = attempt.Manifest; return nil }); err != nil {
		t.Fatal(err)
	}
	// The same challenge cannot be reserved for another otherwise-valid scope.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO validation_fixture_owners VALUES('other','proposal','owner')`); err != nil {
		t.Fatal(err)
	}
	other := attempt
	other.Manifest.RunID = "other"
	if err := s.SetAttempt(ctx, ports.Locator{RunID: "other", ProposalRevision: "proposal"}, other); err == nil {
		t.Fatal("challenge reserved twice")
	}
	if err := s.Cancel(ctx, loc); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, loc, func(a *ports.Attempt) error { a.Cancelled = false; return nil }); !errors.Is(err, domain.ErrUntrusted) {
		t.Fatalf("uncancel=%v", err)
	}
	if _, err := service.Admit(ctx, loc, raw); !errors.Is(err, domain.ErrUntrusted) {
		t.Fatalf("cancelled admitted=%v", err)
	}
	counts(t, s, loc, 0)
}

func TestPostgresUnauthorizedCallerCannotReadOrVerify(t *testing.T) {
	s, _, _, ctx, loc, _, _ := fixture(t)
	called := false
	foreign := context.WithValue(ctx, ownerKey{}, "foreign")
	if _, err := s.UseAttempt(foreign, loc, func(ports.Attempt) (ports.Admission, error) { called = true; return ports.Admission{}, nil }); !errors.Is(err, errDenied) || called {
		t.Fatalf("authorization after verify: %v %t", err, called)
	}
	if _, err := s.GetAdmission(foreign, loc); !errors.Is(err, errDenied) {
		t.Fatal(err)
	}
	if err := s.Cancel(foreign, loc); !errors.Is(err, errDenied) {
		t.Fatal(err)
	}
	counts(t, s, loc, 0)
}

func TestPostgresReceiptFreshnessAtCommitAndLostAcknowledgement(t *testing.T) {
	for _, phase := range []int{2, 3} {
		t.Run(string(rune('0'+phase)), func(t *testing.T) {
			s, service, dsn, ctx, loc, _, raw := fixture(t)
			now := time.Now().UTC().Add(time.Second)
			calls := 0
			s.now = func() time.Time {
				calls++
				if calls >= phase {
					return now.Add(2 * time.Hour)
				}
				return now
			}
			if _, err := service.Admit(ctx, loc, raw); !errors.Is(err, domain.ErrUntrusted) {
				t.Fatalf("expired acknowledged=%v", err)
			}
			want := 0
			if phase == 3 {
				want = 1
			}
			counts(t, s, loc, want)
			if phase == 3 {
				_ = s.Close()
				reopened, err := New(t.Context(), dsn, authorizeOwner)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reopened.Close() })
				api, _ := application.New(reopened)
				if _, err = api.Admit(ctx, loc, raw); !errors.Is(err, ErrUsed) {
					t.Fatalf("lost acknowledgement replay=%v", err)
				}
				counts(t, reopened, loc, 1)
			}
		})
	}
}

func TestPostgresAuthorityRejectsForgedCallbackAndCancelledWrite(t *testing.T) {
	for _, mode := range []string{"forged-id", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, ctx, loc, attempt, raw := fixture(t)
			claim, err := domain.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := domain.Verify(claim, attempt.Manifest, attempt.Policy, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			document, err := domain.Encode(claim)
			if err != nil {
				t.Fatal(err)
			}
			record := ports.Admission{Attestation: document, ID: evidence.ID(), ValueTier: "CONDUIT_NATIVE", State: "VERIFIED_EVIDENCE", Manifest: attempt.Manifest, ProducerID: evidence.Producer(), VerifiedAt: time.Now().UTC(), ExpiresAt: evidence.ExpiresAt()}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			_, err = s.UseAttempt(ctx, loc, func(view ports.Attempt) (ports.Admission, error) {
				// Callback edits cannot replace the store's trusted facts.
				view.Policy.Enabled = false
				if mode == "forged-id" {
					record.ID = "forged"
				} else {
					cancel()
				}
				return record, nil
			})
			if err == nil {
				t.Fatal("invalid callback acknowledged")
			}
			if mode == "forged-id" && !errors.Is(err, domain.ErrUntrusted) {
				t.Fatal(err)
			}
			counts(t, s, loc, 0)
		})
	}
}

func TestPostgresReadPermissionCannotAdmitOrChangeTrustedFacts(t *testing.T) {
	s, service, _, ctx, loc, attempt, raw := fixture(t)
	reader := context.WithValue(ctx, readOnlyKey{}, true)
	if _, err := s.GetAdmission(reader, loc); !errors.Is(err, ErrMissing) {
		t.Fatalf("read scope unavailable: %v", err)
	}
	if _, err := service.Admit(reader, loc, raw); !errors.Is(err, errDenied) {
		t.Fatalf("read scope admitted: %v", err)
	}
	if err := s.Cancel(reader, loc); !errors.Is(err, errDenied) {
		t.Fatalf("read scope cancelled: %v", err)
	}
	if err := s.SetAttempt(reader, loc, attempt); !errors.Is(err, errDenied) {
		t.Fatalf("read scope changed setup: %v", err)
	}
	counts(t, s, loc, 0)
	if _, err := service.Admit(ctx, loc, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAdmission(reader, loc); err != nil {
		t.Fatal(err)
	}
	counts(t, s, loc, 1)
}
