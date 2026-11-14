package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/ports"
)

func testDigest(c string) string { return "sha256:" + strings.Repeat(c, 64) }
func syntheticAttempt(t *testing.T) (ports.Locator, ports.Attempt, []byte, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	profile := testDigest("a")
	tool := domain.Tool{Name: "synthetic-validator", Version: "test-v1", ExecutableDigest: testDigest("b")}
	policy := domain.Policy{Enabled: true, ProfileDigest: profile, MaximumAge: 15 * time.Minute, Checks: map[string]domain.CheckPolicy{"synthetic": {Tool: tool, InputDigest: testDigest("c")}}, Producers: map[string]domain.Producer{domain.ProducerID(public): {PublicKey: public, Profiles: []string{profile}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}}
	policyDigest, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifest := domain.Manifest{RunID: "run", ProposalRevision: "revision", SourceDigest: testDigest("a"), TemplateDigest: testDigest("b"), ProfileDigest: profile, PolicyDigest: policyDigest, ArtifactDigest: testDigest("c"), ToolchainDigest: testDigest("d"), TargetDigest: testDigest("e"), ConfigurationDigest: testDigest("f"), Challenge: strings.Repeat("a", 64)}
	claim := domain.Attestation{Version: domain.Version, ProducerID: domain.ProducerID(public), Manifest: manifest, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Checks: []domain.Check{{ID: "synthetic", Status: "PASS", Tool: tool, InputDigest: testDigest("c"), OutputDigest: testDigest("d"), DiagnosticsDigest: testDigest("e"), StartedAt: now.Add(-2 * time.Minute), FinishedAt: now.Add(-time.Minute)}}}
	payload, err := claim.SigningPayload()
	if err != nil {
		t.Fatal(err)
	}
	claim.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, payload))
	raw, err := domain.Encode(claim)
	if err != nil {
		t.Fatal(err)
	}
	return ports.Locator{RunID: "run", ProposalRevision: "revision"}, ports.Attempt{Manifest: manifest, Policy: policy}, raw, now
}

type authorityFunc func(context.Context, ports.Locator, func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error)

func (f authorityFunc) UseAttempt(ctx context.Context, key ports.Locator, fn func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error) {
	return f(ctx, key, fn)
}
func TestAdmissionRequiresExactlyOneHonestAuthorityCallback(t *testing.T) {
	locator, attempt, raw, now := syntheticAttempt(t)
	for _, mode := range []string{"no-callback", "double-callback", "ignored-error", "forged-record", "expired-return", "clock-regression"} {
		t.Run(mode, func(t *testing.T) {
			clock := now
			authority := authorityFunc(func(_ context.Context, _ ports.Locator, fn func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error) {
				if mode == "no-callback" {
					return ports.Admission{}, nil
				}
				facts := attempt
				if mode == "ignored-error" {
					facts.Cancelled = true
				}
				result, err := fn(facts)
				switch mode {
				case "double-callback":
					_, _ = fn(facts)
				case "ignored-error":
					return result, nil
				case "forged-record":
					result.State = "APPROVED"
				case "expired-return":
					clock = result.ExpiresAt
				case "clock-regression":
					clock = result.VerifiedAt.Add(-time.Nanosecond)
				}
				return result, err
			})
			service, err := New(authority)
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return clock }
			if _, err := service.Admit(t.Context(), locator, raw); err == nil {
				t.Fatal("corrupt authority acknowledged evidence", mode)
			}
		})
	}
}
func TestAdmissionRejectsCurrentCancellationConsumptionAndInputDrift(t *testing.T) {
	for _, mode := range []string{"cancelled", "consumed", "changed-target", "changed-config", "changed-artifact", "changed-challenge", "changed-policy", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			locator, attempt, raw, now := syntheticAttempt(t)
			denied := errors.New("caller forbidden")
			authority := authorityFunc(func(_ context.Context, _ ports.Locator, fn func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error) {
				switch mode {
				case "cancelled":
					attempt.Cancelled = true
				case "consumed":
					attempt.Consumed = true
				case "changed-target":
					attempt.Manifest.TargetDigest = testDigest("f")
				case "changed-config":
					attempt.Manifest.ConfigurationDigest = testDigest("e")
				case "changed-artifact":
					attempt.Manifest.ArtifactDigest = testDigest("d")
				case "changed-challenge":
					attempt.Manifest.Challenge = strings.Repeat("b", 64)
				case "changed-policy":
					attempt.Policy.Enabled = false
				case "unauthorized":
					return ports.Admission{}, denied
				}
				return fn(attempt)
			})
			service, _ := New(authority)
			service.now = func() time.Time { return now }
			if _, err := service.Admit(t.Context(), locator, raw); err == nil {
				t.Fatal("stale/cancelled/ungranted attempt admitted")
			}
		})
	}
}
func TestAdmissionHasNoAuthorityCallForInvalidForeignOrCancelledInput(t *testing.T) {
	locator, _, raw, _ := syntheticAttempt(t)
	calls := 0
	service, _ := New(authorityFunc(func(context.Context, ports.Locator, func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error) {
		calls++
		return ports.Admission{}, nil
	}))
	if _, err := service.Admit(t.Context(), locator, []byte(`{"approved":true}`)); err == nil {
		t.Fatal("unknown approval field accepted")
	}
	foreign := locator
	foreign.RunID = "foreign"
	if _, err := service.Admit(t.Context(), foreign, raw); err == nil {
		t.Fatal("foreign locator accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Admit(ctx, locator, raw); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("invalid input reached authority", calls)
	}
}
func TestAdmissionRecordsOnlyVerifiedEvidence(t *testing.T) {
	locator, attempt, raw, now := syntheticAttempt(t)
	service, _ := New(authorityFunc(func(_ context.Context, _ ports.Locator, fn func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error) {
		return fn(attempt)
	}))
	service.now = func() time.Time { return now }
	record, err := service.Admit(t.Context(), locator, raw)
	if err != nil || record.State != "VERIFIED_EVIDENCE" || record.ValueTier != "CONDUIT_NATIVE" || record.Manifest != attempt.Manifest {
		t.Fatal(record, err)
	}
}
