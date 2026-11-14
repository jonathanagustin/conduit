// Package domain verifies evidence from explicitly trusted validation producers.
// It supplies no approval, replay-consumption store or deployment authority.
package domain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

const Version = "conduit.packaging-validation/v1"
const MaximumChecks = 64
const MaximumLifetime = time.Hour
const MaximumDocumentBytes = 128 << 10

var ErrInvalid = errors.New("invalid packaging validation attestation")
var ErrUntrusted = errors.New("packaging validation evidence is not currently trusted")
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var challengePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)

// Manifest is an exact attempt scope resolved by the receiving authority.
// Challenge must be freshly issued outside the generated artifact workspace.
type Manifest struct {
	RunID               string `json:"runId"`
	ProposalRevision    string `json:"proposalRevision"`
	SourceDigest        string `json:"sourceDigest"`
	TemplateDigest      string `json:"templateDigest"`
	ProfileDigest       string `json:"profileDigest"`
	PolicyDigest        string `json:"policyDigest"`
	ArtifactDigest      string `json:"artifactDigest"`
	ToolchainDigest     string `json:"toolchainDigest"`
	TargetDigest        string `json:"targetDigest"`
	ConfigurationDigest string `json:"configurationDigest"`
	Challenge           string `json:"challenge"`
}

func (m Manifest) Validate() error {
	if !idPattern.MatchString(m.RunID) || !idPattern.MatchString(m.ProposalRevision) || !challengePattern.MatchString(m.Challenge) {
		return ErrInvalid
	}
	for _, d := range []string{m.SourceDigest, m.TemplateDigest, m.ProfileDigest, m.PolicyDigest, m.ArtifactDigest, m.ToolchainDigest, m.TargetDigest, m.ConfigurationDigest} {
		if !digestPattern.MatchString(d) {
			return ErrInvalid
		}
	}
	return nil
}

type Tool struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	ExecutableDigest string `json:"executableDigest"`
	DatabaseDigest   string `json:"databaseDigest,omitempty"`
}

func (t Tool) validate() error {
	if !idPattern.MatchString(t.Name) || strings.TrimSpace(t.Version) == "" || len(t.Version) > 128 || !digestPattern.MatchString(t.ExecutableDigest) || (t.DatabaseDigest != "" && !digestPattern.MatchString(t.DatabaseDigest)) {
		return ErrInvalid
	}
	return nil
}

type Check struct {
	ID                string    `json:"id"`
	Status            string    `json:"status"`
	ExitCode          int       `json:"exitCode"`
	Tool              Tool      `json:"tool"`
	InputDigest       string    `json:"inputDigest"`
	OutputDigest      string    `json:"outputDigest"`
	DiagnosticsDigest string    `json:"diagnosticsDigest"`
	StartedAt         time.Time `json:"startedAt"`
	FinishedAt        time.Time `json:"finishedAt"`
}
type CheckPolicy struct {
	Tool        Tool   `json:"tool"`
	InputDigest string `json:"inputDigest"`
}
type Producer struct {
	PublicKey []byte    `json:"publicKey"`
	Profiles  []string  `json:"profiles"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
}

// Policy comes from trusted operator configuration, never a receipt/body field.
// Digest binds its enabled state, required checks, tool pins and producer scope.
type Policy struct {
	Enabled       bool                   `json:"enabled"`
	ProfileDigest string                 `json:"profileDigest"`
	MaximumAge    time.Duration          `json:"maximumAge"`
	Checks        map[string]CheckPolicy `json:"checks"`
	Producers     map[string]Producer    `json:"producers"`
}

func ProducerID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return "validator-sha256:" + hex.EncodeToString(sum[:])
}
func (p Policy) Validate() error {
	if !digestPattern.MatchString(p.ProfileDigest) || p.MaximumAge <= 0 || p.MaximumAge > MaximumLifetime || len(p.Checks) == 0 || len(p.Checks) > MaximumChecks || len(p.Producers) == 0 || len(p.Producers) > 32 {
		return ErrInvalid
	}
	for id, c := range p.Checks {
		if !idPattern.MatchString(id) || c.Tool.validate() != nil || !digestPattern.MatchString(c.InputDigest) {
			return ErrInvalid
		}
	}
	for id, producer := range p.Producers {
		if len(producer.PublicKey) != ed25519.PublicKeySize || id != ProducerID(producer.PublicKey) || producer.NotBefore.IsZero() || !producer.NotAfter.After(producer.NotBefore) || len(producer.Profiles) == 0 || len(producer.Profiles) > 32 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, profile := range producer.Profiles {
			if !digestPattern.MatchString(profile) || seen[profile] {
				return ErrInvalid
			}
			seen[profile] = true
		}
	}
	return nil
}
func (p Policy) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	normalized := p
	normalized.Producers = make(map[string]Producer, len(p.Producers))
	for id, producer := range p.Producers {
		producer.Profiles = slices.Clone(producer.Profiles)
		slices.Sort(producer.Profiles)
		producer.NotBefore = producer.NotBefore.UTC()
		producer.NotAfter = producer.NotAfter.UTC()
		normalized.Producers[id] = producer
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Attestation authenticates a producer's claims, not an honest tool execution
// or a deployment decision. Runtime checks must really be run by that trusted
// producer; the receiver must independently authorize and consume the attempt.
// CONDUIT-INVENTED: packagevalidation.Attestation
// Displaced-by: a reviewed replacement packaging validation producer, exact-scope evidence and freshness contract.
type Attestation struct {
	Version    string    `json:"version"`
	ProducerID string    `json:"producerId"`
	Manifest   Manifest  `json:"manifest"`
	IssuedAt   time.Time `json:"issuedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Checks     []Check   `json:"checks"`
	Signature  string    `json:"signature"`
}

func (a Attestation) validate() error {
	if a.Version != Version || !strings.HasPrefix(a.ProducerID, "validator-sha256:") || len(a.ProducerID) != len("validator-sha256:")+64 || a.Manifest.Validate() != nil || a.IssuedAt.IsZero() || !a.ExpiresAt.After(a.IssuedAt) || a.ExpiresAt.Sub(a.IssuedAt) > MaximumLifetime || len(a.Checks) == 0 || len(a.Checks) > MaximumChecks {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, c := range a.Checks {
		if !idPattern.MatchString(c.ID) || seen[c.ID] || c.Tool.validate() != nil || !digestPattern.MatchString(c.InputDigest) || !digestPattern.MatchString(c.OutputDigest) || !digestPattern.MatchString(c.DiagnosticsDigest) || c.StartedAt.IsZero() || c.FinishedAt.Before(c.StartedAt) || c.FinishedAt.After(a.IssuedAt) || c.ExitCode < -1 || c.ExitCode > 255 {
			return ErrInvalid
		}
		seen[c.ID] = true
		if !slices.Contains([]string{"PASS", "FAIL", "UNKNOWN", "BLOCKED", "NOT_RUN"}, c.Status) {
			return ErrInvalid
		}
	}
	return nil
}

// SigningPayload is domain-separated canonical JSON of the typed claim. Check
// order is normalized. This verifier deliberately supplies no signing key API.
func (a Attestation) SigningPayload() ([]byte, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	a = a.normalized()
	a.Signature = ""
	body, err := json.Marshal(a)
	if err != nil || len(body) > MaximumDocumentBytes {
		return nil, ErrInvalid
	}
	return append([]byte(Version+"\n"), body...), nil
}

func (a Attestation) normalized() Attestation {
	a.IssuedAt = a.IssuedAt.UTC()
	a.ExpiresAt = a.ExpiresAt.UTC()
	a.Checks = slices.Clone(a.Checks)
	slices.SortFunc(a.Checks, func(x, y Check) int { return strings.Compare(x.ID, y.ID) })
	for i := range a.Checks {
		a.Checks[i].StartedAt = a.Checks[i].StartedAt.UTC()
		a.Checks[i].FinishedAt = a.Checks[i].FinishedAt.UTC()
	}
	return a
}

// Evidence is an opaque verification observation. It is not execution authority.
type Evidence struct {
	id, producer          string
	manifest              Manifest
	verifiedAt, expiresAt time.Time
}

func (e Evidence) ExpiresAt() time.Time { return e.expiresAt }
func (e Evidence) ID() string           { return e.id }
func (e Evidence) Producer() string     { return e.producer }
func (e Evidence) Manifest() Manifest   { return e.manifest }
func (e Evidence) ValidAt(at time.Time) bool {
	return e.id != "" && !at.Before(e.verifiedAt) && at.Before(e.expiresAt)
}
func Verify(a Attestation, expected Manifest, policy Policy, now time.Time) (Evidence, error) {
	if expected.Validate() != nil || policy.Validate() != nil {
		return Evidence{}, ErrInvalid
	}
	payload, err := a.SigningPayload()
	if err != nil {
		return Evidence{}, err
	}
	digest, err := policy.Digest()
	if err != nil {
		return Evidence{}, err
	}
	if !policy.Enabled || a.Manifest != expected || expected.PolicyDigest != digest || expected.ProfileDigest != policy.ProfileDigest || now.Before(a.IssuedAt) || !now.Before(a.ExpiresAt) || a.ExpiresAt.Sub(a.IssuedAt) > policy.MaximumAge || now.Sub(a.IssuedAt) > policy.MaximumAge {
		return Evidence{}, ErrUntrusted
	}
	producer, ok := policy.Producers[a.ProducerID]
	if !ok || now.Before(producer.NotBefore) || !now.Before(producer.NotAfter) || a.IssuedAt.Before(producer.NotBefore) || !slices.Contains(producer.Profiles, policy.ProfileDigest) {
		return Evidence{}, ErrUntrusted
	}
	if len(a.Signature) != base64.RawStdEncoding.EncodedLen(ed25519.SignatureSize) {
		return Evidence{}, ErrUntrusted
	}
	signature, err := base64.RawStdEncoding.Strict().DecodeString(a.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(producer.PublicKey, payload, signature) {
		return Evidence{}, ErrUntrusted
	}
	if len(a.Checks) != len(policy.Checks) {
		return Evidence{}, ErrUntrusted
	}
	expires := a.ExpiresAt.UTC()
	if producer.NotAfter.Before(expires) {
		expires = producer.NotAfter.UTC()
	}
	for _, check := range a.Checks {
		required, ok := policy.Checks[check.ID]
		if !ok || check.Status != "PASS" || check.ExitCode != 0 || check.Tool != required.Tool || check.InputDigest != required.InputDigest || now.Before(check.FinishedAt) || now.Sub(check.StartedAt) > policy.MaximumAge {
			return Evidence{}, ErrUntrusted
		}
		checkExpiry := check.StartedAt.Add(policy.MaximumAge)
		if checkExpiry.Before(expires) {
			expires = checkExpiry
		}
	}
	if !now.Before(expires) {
		return Evidence{}, ErrUntrusted
	}
	sum := sha256.Sum256(payload)
	return Evidence{"vrc_" + hex.EncodeToString(sum[:]), a.ProducerID, expected, now, expires}, nil
}
