package domain

import (
	"crypto/x509"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"
)

var revocationInstant = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func credentialIssued(environment, agent, serial string, notBefore time.Time) AgentCredential {
	return AgentCredential{
		Identity: AgentIdentity{Environment: environment, AgentID: agent}, Serial: serial,
		NotBefore: notBefore, NotAfter: notBefore.Add(90 * 24 * time.Hour),
	}
}

func mustRevocation(t *testing.T, draft AgentRevocation) AgentRevocation {
	t.Helper()
	revocation, err := NewAgentRevocation(draft, "alice", revocationInstant)
	if err != nil {
		t.Fatal(err)
	}
	return revocation
}

func TestASerialRevocationRefusesExactlyOneCertificate(t *testing.T) {
	revocation := mustRevocation(t, AgentRevocation{
		Scope: RevocationSerial, Environment: "prod", Serial: "serial=0A:1B", Reason: "leaked",
	})
	if revocation.Serial != "a1b" {
		t.Fatalf("serial normalized to %q, want a1b", revocation.Serial)
	}
	old := revocationInstant.Add(-time.Hour)
	if !revocation.Refuses(credentialIssued("prod", "primary", "a1b", old)) {
		t.Fatal("the revoked serial was admitted")
	}
	// The same Agent's other certificate, issued at the same time, is untouched.
	if revocation.Refuses(credentialIssued("prod", "primary", "a1c", old)) {
		t.Fatal("a serial revocation refused a different certificate")
	}
	// A serial is unique per issuer, not across Environments.
	if revocation.Refuses(credentialIssued("staging", "primary", "a1b", old)) {
		t.Fatal("a serial revocation reached into another Environment")
	}
	if revocation.Refuses(credentialIssued("prod", "primary", "", old)) {
		t.Fatal("a credential with no certificate matched a serial")
	}
}

func TestAnAgentRevocationRefusesUntilACertificateIsIssuedAfterIt(t *testing.T) {
	revocation := mustRevocation(t, AgentRevocation{
		Scope: RevocationAgent, Environment: "prod", AgentID: "primary", Reason: "key leaked",
	})
	if !revocation.NotBefore.Equal(revocationInstant) {
		t.Fatalf("default instant = %s, want the moment of revocation", revocation.NotBefore)
	}
	for name, test := range map[string]struct {
		credential AgentCredential
		refused    bool
	}{
		"the certificate held at revocation":      {credentialIssued("prod", "primary", "a1", revocationInstant.Add(-time.Hour)), true},
		"a certificate issued at the instant":     {credentialIssued("prod", "primary", "a2", revocationInstant), true},
		"a certificate issued after the instant":  {credentialIssued("prod", "primary", "a3", revocationInstant.Add(time.Second)), false},
		"another Agent in the Environment":        {credentialIssued("prod", "secondary", "b1", revocationInstant.Add(-time.Hour)), false},
		"the same Agent name elsewhere":           {credentialIssued("staging", "primary", "c1", revocationInstant.Add(-time.Hour)), false},
		"development headers with no certificate": {AgentCredential{Identity: AgentIdentity{Environment: "prod", AgentID: "primary"}}, true},
	} {
		if got := revocation.Refuses(test.credential); got != test.refused {
			t.Errorf("%s: refused = %t, want %t", name, got, test.refused)
		}
	}
}

func TestAnEnvironmentRevocationRefusesEveryAgentInItAndNoOther(t *testing.T) {
	revocation := mustRevocation(t, AgentRevocation{Scope: RevocationEnvironment, Environment: "prod", Reason: "incident"})
	old := revocationInstant.Add(-time.Hour)
	if !revocation.Refuses(credentialIssued("prod", "primary", "a1", old)) || !revocation.Refuses(credentialIssued("prod", "secondary", "b1", old)) {
		t.Fatal("an Agent of a revoked Environment was admitted")
	}
	if revocation.Refuses(credentialIssued("staging", "primary", "c1", old)) {
		t.Fatal("an Environment revocation refused another Environment")
	}
	if revocation.Refuses(credentialIssued("prod", "primary", "a2", revocationInstant.Add(time.Minute))) {
		t.Fatal("a certificate issued after the Environment revocation was refused")
	}
}

func TestAnExplicitInstantOutlivesReissue(t *testing.T) {
	// Decommissioning: an instant far in the future refuses every certificate
	// any issuer could plausibly sign.
	revocation := mustRevocation(t, AgentRevocation{
		Scope: RevocationAgent, Environment: "prod", AgentID: "primary", Reason: "decommissioned",
		NotBefore: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if !revocation.Refuses(credentialIssued("prod", "primary", "a9", revocationInstant.Add(365*24*time.Hour))) {
		t.Fatal("a reissued certificate was admitted past an explicit instant")
	}
}

func TestAScopeThisCodeDoesNotKnowFailsClosedInItsEnvironment(t *testing.T) {
	revocation := AgentRevocation{Scope: "TRUST_DOMAIN", Environment: "prod"}
	if !revocation.Refuses(credentialIssued("prod", "primary", "a1", revocationInstant.Add(time.Hour))) {
		t.Fatal("an unreadable deny-list entry was read as denying nothing")
	}
	if revocation.Refuses(credentialIssued("staging", "primary", "a1", revocationInstant)) {
		t.Fatal("an unreadable entry reached into another Environment")
	}
}

func TestRevokingTheSameThingTwiceIsOneRecord(t *testing.T) {
	first := mustRevocation(t, AgentRevocation{Scope: "agent", Environment: "prod", AgentID: "primary", Reason: "first"})
	second, err := NewAgentRevocation(AgentRevocation{Scope: RevocationAgent, Environment: "prod", AgentID: "primary", Reason: "second"},
		"bob", revocationInstant.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ID[:4] != "arv_" {
		t.Fatalf("identifiers %q and %q; the same target must be one record", first.ID, second.ID)
	}
	other := mustRevocation(t, AgentRevocation{Scope: RevocationAgent, Environment: "prod", AgentID: "secondary", Reason: "other"})
	environment := mustRevocation(t, AgentRevocation{Scope: RevocationEnvironment, Environment: "prod", Reason: "other"})
	if other.ID == first.ID || environment.ID == first.ID {
		t.Fatal("different targets share an identifier")
	}
}

func TestARevocationAnOperatorCouldNotHaveMeantIsRefused(t *testing.T) {
	for name, draft := range map[string]AgentRevocation{
		"no reason":                     {Scope: RevocationEnvironment, Environment: "prod"},
		"unknown scope":                 {Scope: "EVERYTHING", Environment: "prod", Reason: "r"},
		"environment not a label":       {Scope: RevocationEnvironment, Environment: "Prod/../x", Reason: "r"},
		"agent scope without an agent":  {Scope: RevocationAgent, Environment: "prod", Reason: "r"},
		"agent scope with a serial":     {Scope: RevocationAgent, Environment: "prod", AgentID: "primary", Serial: "0a", Reason: "r"},
		"serial scope without a serial": {Scope: RevocationSerial, Environment: "prod", Reason: "r"},
		"serial that is not hex":        {Scope: RevocationSerial, Environment: "prod", Serial: "not-hex", Reason: "r"},
		"serial scope with an instant":  {Scope: RevocationSerial, Environment: "prod", Serial: "0a", NotBefore: revocationInstant, Reason: "r"},
		"environment scope with agent":  {Scope: RevocationEnvironment, Environment: "prod", AgentID: "primary", Reason: "r"},
		"reason past the bound":         {Scope: RevocationEnvironment, Environment: "prod", Reason: string(make([]byte, MaximumRevocationReasonBytes+1))},
	} {
		if _, err := NewAgentRevocation(draft, "alice", revocationInstant); !errors.Is(err, ErrRevocationInvalid) {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	if _, err := NewAgentRevocation(AgentRevocation{Scope: RevocationEnvironment, Environment: "prod", Reason: "r"}, " ", revocationInstant); !errors.Is(err, ErrRevocationInvalid) {
		t.Errorf("a revocation with no acting operator was accepted: %v", err)
	}
}

func TestTheCredentialCarriesTheCertificateFactsRevocationDecidesOn(t *testing.T) {
	identity, _ := url.Parse("spiffe://platform.delivery/environment/prod/agent/primary")
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(0xA1B), URIs: []*url.URL{identity},
		NotBefore: revocationInstant.Add(-time.Hour), NotAfter: revocationInstant.Add(time.Hour),
	}
	credential, err := AgentCredentialFromCertificate(certificate)
	if err != nil {
		t.Fatal(err)
	}
	want := AgentCredential{
		Identity: AgentIdentity{Environment: "prod", AgentID: "primary"}, Serial: "a1b",
		NotBefore: revocationInstant.Add(-time.Hour), NotAfter: revocationInstant.Add(time.Hour),
	}
	if credential != want {
		t.Fatalf("credential = %#v, want %#v", credential, want)
	}
	// The serial an operator copies from openssl is the serial the Hub compares.
	if normalized, err := NormalizeCertificateSerial("0A1B"); err != nil || normalized != credential.Serial {
		t.Fatalf("operator spelling normalized to %q, %v; certificate reads %q", normalized, err, credential.Serial)
	}
}
