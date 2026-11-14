package domain

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func digest(letter string) string { return "sha256:" + strings.Repeat(letter, 64) }
func fixture(t *testing.T) (Attestation, Manifest, Policy, ed25519.PrivateKey, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tool := Tool{Name: "schema-validator", Version: "test-v1", ExecutableDigest: digest("a")}
	policy := Policy{Enabled: true, ProfileDigest: digest("b"), MaximumAge: 15 * time.Minute, Checks: map[string]CheckPolicy{"schemas": {Tool: tool, InputDigest: digest("c")}, "scanner": {Tool: Tool{Name: "scanner", Version: "test-v1", ExecutableDigest: digest("d"), DatabaseDigest: digest("e")}, InputDigest: digest("c")}}, Producers: map[string]Producer{ProducerID(public): {PublicKey: public, Profiles: []string{digest("b")}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}}
	policyDigest, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{RunID: "run-1", ProposalRevision: "proposal-1", SourceDigest: digest("a"), TemplateDigest: digest("b"), ProfileDigest: policy.ProfileDigest, PolicyDigest: policyDigest, ArtifactDigest: digest("c"), ToolchainDigest: digest("d"), TargetDigest: digest("e"), ConfigurationDigest: digest("f"), Challenge: strings.Repeat("a", 64)}
	receipt := Attestation{Version: Version, ProducerID: ProducerID(public), Manifest: manifest, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(5 * time.Minute)}
	for id, check := range policy.Checks {
		receipt.Checks = append(receipt.Checks, Check{ID: id, Status: "PASS", ExitCode: 0, Tool: check.Tool, InputDigest: check.InputDigest, OutputDigest: digest("a"), DiagnosticsDigest: digest("b"), StartedAt: now.Add(-2 * time.Minute), FinishedAt: now.Add(-time.Minute)})
	}
	sign(t, &receipt, private)
	return receipt, manifest, policy, private, now
}
func sign(t *testing.T, a *Attestation, key ed25519.PrivateKey) {
	t.Helper()
	payload, err := a.SigningPayload()
	if err != nil {
		t.Fatal(err)
	}
	a.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, payload))
}
func TestAuthenticatedEvidenceBindsCurrentInputsAndRemainsOpaque(t *testing.T) {
	receipt, manifest, policy, _, now := fixture(t)
	evidence, err := Verify(receipt, manifest, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ID() == "" || evidence.Producer() != receipt.ProducerID || evidence.Manifest() != manifest || !evidence.ValidAt(now) || evidence.ValidAt(receipt.ExpiresAt) || evidence.ValidAt(now.Add(-time.Nanosecond)) {
		t.Fatal("wrong evidence binding", evidence)
	}
	//lint:ignore SA9005 Opaque evidence must never become portable authority.
	raw, err := json.Marshal(evidence)
	if err != nil || string(raw) != "{}" {
		t.Fatal("evidence serialized as authority", string(raw), err)
	}
	changed := receipt
	changed.Checks = append([]Check{}, receipt.Checks...)
	changed.Checks[0], changed.Checks[1] = changed.Checks[1], changed.Checks[0]
	equivalent, err := Verify(changed, manifest, policy, now)
	if err != nil || equivalent.ID() != evidence.ID() {
		t.Fatal("order changed the authenticated identity", err)
	}
}
func TestChangedCurrentScopeCannotReuseAttestation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"run", func(m *Manifest) { m.RunID = "other" }}, {"proposal", func(m *Manifest) { m.ProposalRevision = "other" }},
		{"source", func(m *Manifest) { m.SourceDigest = digest("f") }}, {"template", func(m *Manifest) { m.TemplateDigest = digest("f") }},
		{"profile", func(m *Manifest) { m.ProfileDigest = digest("f") }}, {"policy", func(m *Manifest) { m.PolicyDigest = digest("f") }},
		{"artifact", func(m *Manifest) { m.ArtifactDigest = digest("f") }}, {"tools", func(m *Manifest) { m.ToolchainDigest = digest("f") }},
		{"target", func(m *Manifest) { m.TargetDigest = digest("f") }}, {"config", func(m *Manifest) { m.ConfigurationDigest = digest("e") }},
		{"challenge", func(m *Manifest) { m.Challenge = strings.Repeat("b", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, m, p, _, now := fixture(t)
			test.mutate(&m)
			if _, err := Verify(a, m, p, now); err == nil {
				t.Fatal("changed current scope accepted")
			}
		})
	}
}
func TestTrustedSignatureCannotTurnUnknownOrIncompleteChecksIntoPass(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Attestation)
	}{
		{"fail", func(a *Attestation) { a.Checks[0].Status = "FAIL" }}, {"unknown", func(a *Attestation) { a.Checks[0].Status = "UNKNOWN" }},
		{"blocked", func(a *Attestation) { a.Checks[0].Status = "BLOCKED" }}, {"not-run", func(a *Attestation) { a.Checks[0].Status = "NOT_RUN" }},
		{"nonzero", func(a *Attestation) { a.Checks[0].ExitCode = 1 }}, {"missing-tool", func(a *Attestation) { a.Checks[0].ExitCode = -1 }},
		{"missing-check", func(a *Attestation) { a.Checks = a.Checks[:1] }}, {"unknown-check", func(a *Attestation) { a.Checks[0].ID = "self-selected" }},
		{"tool-pin", func(a *Attestation) { a.Checks[0].Tool.ExecutableDigest = digest("f") }},
		{"tool-version", func(a *Attestation) { a.Checks[0].Tool.Version = "other" }},
		{"input", func(a *Attestation) { a.Checks[0].InputDigest = digest("f") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, m, p, key, now := fixture(t)
			test.mutate(&a)
			sign(t, &a, key)
			if _, err := Verify(a, m, p, now); err == nil {
				t.Fatal("signed incomplete/unknown result admitted")
			}
		})
	}
	a, m, p, key, now := fixture(t)
	for i := range a.Checks {
		if a.Checks[i].ID == "scanner" {
			a.Checks[i].Tool.DatabaseDigest = ""
		}
	}
	sign(t, &a, key)
	if _, err := Verify(a, m, p, now); err == nil {
		t.Fatal("missing scanner database admitted")
	}
}
func TestProducerTrustRevocationAndTimeBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Attestation, *Policy, *time.Time)
	}{
		{"unsigned", func(a *Attestation, _ *Policy, _ *time.Time) { a.Signature = "" }},
		{"forged", func(a *Attestation, _ *Policy, _ *time.Time) { a.Signature = strings.Repeat("a", 86) }},
		{"huge-signature", func(a *Attestation, _ *Policy, _ *time.Time) {
			a.Signature = strings.Repeat("a", 2*MaximumDocumentBytes)
		}},
		{"expired", func(a *Attestation, _ *Policy, n *time.Time) { *n = a.ExpiresAt }},
		{"future", func(a *Attestation, _ *Policy, n *time.Time) { *n = a.IssuedAt.Add(-time.Nanosecond) }},
		{"disabled-profile", func(_ *Attestation, p *Policy, _ *time.Time) { p.Enabled = false }},
		{"rotated-key", func(a *Attestation, p *Policy, _ *time.Time) {
			producer := p.Producers[a.ProducerID]
			producer.PublicKey = bytes.Repeat([]byte{1}, 32)
			delete(p.Producers, a.ProducerID)
			p.Producers[ProducerID(producer.PublicKey)] = producer
		}},
		{"profile-scope", func(a *Attestation, p *Policy, _ *time.Time) {
			producer := p.Producers[a.ProducerID]
			producer.Profiles = []string{digest("f")}
			p.Producers[a.ProducerID] = producer
		}},
		{"revoked-at-boundary", func(a *Attestation, p *Policy, n *time.Time) {
			producer := p.Producers[a.ProducerID]
			producer.NotAfter = *n
			p.Producers[a.ProducerID] = producer
		}},
		{"required-checks-changed", func(_ *Attestation, p *Policy, _ *time.Time) { delete(p.Checks, "scanner") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, m, p, _, now := fixture(t)
			test.mutate(&a, &p, &now)
			if _, err := Verify(a, m, p, now); err == nil {
				t.Fatal("untrusted/stale result admitted")
			}
		})
	}
}
func TestFreshWrapperCannotRefreshStaleChecks(t *testing.T) {
	a, m, p, key, now := fixture(t)
	a.Checks[0].StartedAt = now.Add(-p.MaximumAge - time.Nanosecond)
	sign(t, &a, key)
	if _, err := Verify(a, m, p, now); err == nil {
		t.Fatal("fresh wrapper refreshed an old check")
	}
}
func TestStrictReceiptCodecRejectsAmbiguousAndAuthorityFields(t *testing.T) {
	a, m, p, _, now := fixture(t)
	raw, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(decoded, m, p, now); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, raw...), []byte(` {}`)...), bytes.Replace(raw, []byte(`"version":`), []byte(`"approved":true,"version":`), 1), bytes.Replace(raw, []byte(`"version":`), []byte(`"version":"forged","version":`), 1), bytes.Repeat([]byte{'x'}, MaximumDocumentBytes+1), []byte(`null`)} {
		if _, err := Decode(bad); err == nil {
			t.Fatal("ambiguous/authority document accepted")
		}
	}
}

func TestSignatureAuthenticatesClaimBytesAndProtocol(t *testing.T) {
	for _, mode := range []string{"tampered-output", "tampered-diagnostics", "wrong-protocol", "duplicate-check"} {
		t.Run(mode, func(t *testing.T) {
			a, m, p, key, now := fixture(t)
			switch mode {
			case "tampered-output":
				a.Checks[0].OutputDigest = digest("f")
			case "tampered-diagnostics":
				a.Checks[0].DiagnosticsDigest = digest("f")
			case "wrong-protocol":
				payload, err := a.SigningPayload()
				if err != nil {
					t.Fatal(err)
				}
				a.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, payload[len(Version)+1:]))
			case "duplicate-check":
				a.Checks = append(a.Checks, a.Checks[0])
			}
			if _, err := Verify(a, m, p, now); err == nil {
				t.Fatal("unauthenticated claim admitted")
			}
		})
	}
}
func TestCanonicalClaimNormalizesEquivalentTimes(t *testing.T) {
	a, m, p, _, now := fixture(t)
	first, err := Verify(a, m, p, now)
	if err != nil {
		t.Fatal(err)
	}
	zone := time.FixedZone("equivalent", 2*60*60)
	a.IssuedAt = a.IssuedAt.In(zone)
	a.ExpiresAt = a.ExpiresAt.In(zone)
	for i := range a.Checks {
		a.Checks[i].StartedAt = a.Checks[i].StartedAt.In(zone)
		a.Checks[i].FinishedAt = a.Checks[i].FinishedAt.In(zone)
	}
	for id, producer := range p.Producers {
		producer.NotBefore = producer.NotBefore.In(zone)
		producer.NotAfter = producer.NotAfter.In(zone)
		p.Producers[id] = producer
	}
	second, err := Verify(a, m, p, now)
	if err != nil || first.ID() != second.ID() {
		t.Fatal("equivalent timestamp changed authentication", err)
	}
}

func TestProducerProfileScopeAndKeyExpiryAreCheckedWithMatchingPolicyDigest(t *testing.T) {
	a, m, p, key, now := fixture(t)
	producer := p.Producers[a.ProducerID]
	producer.Profiles = []string{digest("f")}
	p.Producers[a.ProducerID] = producer
	policyDigest, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	m.PolicyDigest = policyDigest
	a.Manifest = m
	sign(t, &a, key)
	if _, err := Verify(a, m, p, now); err == nil {
		t.Fatal("trusted key validated an unauthorized profile")
	}
	a, m, p, key, now = fixture(t)
	producer = p.Producers[a.ProducerID]
	producer.NotAfter = now.Add(time.Minute)
	p.Producers[a.ProducerID] = producer
	policyDigest, err = p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	m.PolicyDigest = policyDigest
	a.Manifest = m
	sign(t, &a, key)
	evidence, err := Verify(a, m, p, now)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ValidAt(producer.NotAfter) {
		t.Fatal("evidence outlived the key")
	}
	if _, err := Verify(a, m, p, producer.NotAfter); err == nil {
		t.Fatal("key end boundary accepted")
	}
}
