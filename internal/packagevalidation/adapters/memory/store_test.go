package memory

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/application"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/ports"
)

func digest(c string) string { return "sha256:" + strings.Repeat(c, 64) }
func fixture(t *testing.T) (*Store, *application.Service, ports.Locator, ports.Attempt, []byte) {
	t.Helper()
	now := time.Now().UTC()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tool := domain.Tool{Name: "synthetic", Version: "test-v1", ExecutableDigest: digest("a")}
	policy := domain.Policy{Enabled: true, ProfileDigest: digest("b"), MaximumAge: 15 * time.Minute, Checks: map[string]domain.CheckPolicy{"check": {Tool: tool, InputDigest: digest("c")}}, Producers: map[string]domain.Producer{domain.ProducerID(public): {PublicKey: public, Profiles: []string{digest("b")}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}}
	policyDigest, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifest := domain.Manifest{RunID: "run", ProposalRevision: "proposal", SourceDigest: digest("a"), TemplateDigest: digest("b"), ProfileDigest: digest("b"), PolicyDigest: policyDigest, ArtifactDigest: digest("c"), ToolchainDigest: digest("d"), TargetDigest: digest("e"), ConfigurationDigest: digest("f"), Challenge: strings.Repeat("a", 64)}
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
	store, err := New(func(context.Context, ports.Locator) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	locator := ports.Locator{RunID: manifest.RunID, ProposalRevision: manifest.ProposalRevision}
	attempt := ports.Attempt{Manifest: manifest, Policy: policy}
	if err := store.SetAttempt(locator, attempt); err != nil {
		t.Fatal(err)
	}
	service, err := application.New(store)
	if err != nil {
		t.Fatal(err)
	}
	return store, service, locator, attempt, raw
}
func TestConcurrentAdmissionConsumesChallengeAndAuditsExactlyOnce(t *testing.T) {
	store, service, locator, _, raw := fixture(t)
	var wg sync.WaitGroup
	var successes atomic.Int32
	for range 24 {
		wg.Go(func() {
			if _, err := service.Admit(t.Context(), locator, raw); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrUsed) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("challenge reused", successes.Load())
	}
	store.mu.Lock()
	records, audits, used := len(store.records), len(store.audits), store.attempts[locator].Consumed
	store.mu.Unlock()
	if records != 1 || audits != 1 || !used {
		t.Fatal("non-atomic admission", records, audits, used)
	}
	record, err := store.GetAdmission(t.Context(), locator)
	if err != nil || record.State != "VERIFIED_EVIDENCE" {
		t.Fatal(record, err)
	}
}
func TestAuditFailureAndCommitExpiryDoNotConsumeAttempt(t *testing.T) {
	for _, mode := range []string{"audit", "expired", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			store, service, locator, _, raw := fixture(t)
			failure := errors.New("audit persistence failed")
			switch mode {
			case "audit":
				store.auditFailure = failure
			case "expired":
				store.now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
			case "cancelled":
				if err := store.Cancel(locator); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := service.Admit(t.Context(), locator, raw); err == nil {
				t.Fatal("failed admission acknowledged")
			}
			store.mu.Lock()
			records, audits, used := len(store.records), len(store.audits), store.attempts[locator].Consumed
			store.mu.Unlock()
			if records != 0 || audits != 0 || used {
				t.Fatal("failed admission persisted", records, audits, used)
			}
		})
	}
}
func TestTrustedSetupCannotResetConsumedAttemptAndCopiesPolicy(t *testing.T) {
	store, service, locator, attempt, raw := fixture(t)
	original := cloneAttempt(attempt)
	// Changes to the caller's setup maps cannot mutate current trusted state.
	delete(attempt.Policy.Checks, "check")
	if _, err := service.Admit(t.Context(), locator, raw); err != nil {
		t.Fatal("caller mutated stored policy", err)
	}
	if err := store.SetAttempt(locator, original); err == nil {
		t.Fatal("consumed attempt reset")
	}
}
func TestUnauthorizedAdmissionAndHistoricalReadAreDenied(t *testing.T) {
	store, service, locator, _, raw := fixture(t)
	denied := errors.New("caller forbidden")
	store.authorize = func(context.Context, ports.Locator) error { return denied }
	if _, err := service.Admit(t.Context(), locator, raw); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if _, err := store.GetAdmission(t.Context(), locator); !errors.Is(err, denied) {
		t.Fatal(err)
	}
}

func TestAdmissionRetainsCanonicalSignedChecksWithoutMutableAliases(t *testing.T) {
	store, service, locator, attempt, raw := fixture(t)
	record, err := service.Admit(t.Context(), locator, raw)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := domain.Decode(record.Attestation)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := domain.Verify(claim, attempt.Manifest, attempt.Policy, time.Now().UTC())
	if err != nil || evidence.ID() != record.ID || len(claim.Checks) != 1 || claim.Checks[0].Tool.Version != "test-v1" || claim.Checks[0].OutputDigest == "" {
		t.Fatal("signed check evidence lost", err)
	}
	record.Attestation[0] = 'x'
	stored, err := store.GetAdmission(t.Context(), locator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.Decode(stored.Attestation); err != nil {
		t.Fatal("caller changed stored evidence", err)
	}
	stored.Attestation[0] = 'x'
	again, err := store.GetAdmission(t.Context(), locator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.Decode(again.Attestation); err != nil {
		t.Fatal("historical reader changed stored evidence", err)
	}
}
func TestFinalStoreCheckRejectsForgedOrMissingEvidenceDocument(t *testing.T) {
	for _, mode := range []string{"missing", "forged-id", "forged-producer"} {
		t.Run(mode, func(t *testing.T) {
			store, _, locator, attempt, raw := fixture(t)
			now := time.Now().UTC()
			claim, err := domain.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := domain.Verify(claim, attempt.Manifest, attempt.Policy, now)
			if err != nil {
				t.Fatal(err)
			}
			record := ports.Admission{Attestation: raw, ID: evidence.ID(), ValueTier: "CONDUIT_NATIVE", State: "VERIFIED_EVIDENCE", Manifest: attempt.Manifest, ProducerID: evidence.Producer(), VerifiedAt: now, ExpiresAt: evidence.ExpiresAt()}
			switch mode {
			case "missing":
				record.Attestation = nil
			case "forged-id":
				record.ID = "forged"
			case "forged-producer":
				record.ProducerID = "forged"
			}
			if _, err := store.UseAttempt(t.Context(), locator, func(ports.Attempt) (ports.Admission, error) { return record, nil }); err == nil {
				t.Fatal("forged final record admitted")
			}
			if len(store.records) != 0 || len(store.audits) != 0 || store.attempts[locator].Consumed {
				t.Fatal("failed final check persisted state")
			}
		})
	}
}

func TestCurrentPolicyAndTargetRefreshInvalidateOldSignedResult(t *testing.T) {
	for _, mode := range []string{"policy-disabled", "config-changed", "target-changed"} {
		t.Run(mode, func(t *testing.T) {
			store, service, locator, _, raw := fixture(t)
			if err := store.Update(locator, func(attempt *ports.Attempt) error {
				switch mode {
				case "policy-disabled":
					attempt.Policy.Enabled = false
					sum, err := attempt.Policy.Digest()
					if err != nil {
						return err
					}
					attempt.Manifest.PolicyDigest = sum
				case "config-changed":
					attempt.Manifest.ConfigurationDigest = digest("e")
				case "target-changed":
					attempt.Manifest.TargetDigest = digest("f")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Admit(t.Context(), locator, raw); err == nil {
				t.Fatal("old result survived current-fact refresh")
			}
			if len(store.records) != 0 || len(store.audits) != 0 || store.attempts[locator].Consumed {
				t.Fatal("stale result persisted")
			}
		})
	}
}
func TestCurrentFactsCannotUnconsumeRotateOrUndoCancellation(t *testing.T) {
	store, service, locator, _, raw := fixture(t)
	if _, err := service.Admit(t.Context(), locator, raw); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(locator, func(a *ports.Attempt) error { a.Consumed = false; return nil }); err == nil {
		t.Fatal("challenge unconsumed")
	}
	if err := store.Update(locator, func(a *ports.Attempt) error { a.Manifest.Challenge = strings.Repeat("b", 64); return nil }); err == nil {
		t.Fatal("challenge rotated inside existing attempt")
	}
	if err := store.Cancel(locator); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(locator, func(a *ports.Attempt) error { a.Cancelled = false; return nil }); err == nil {
		t.Fatal("cancellation undone")
	}
}
