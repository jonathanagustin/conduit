package domain

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// AgentCredential is what an Agent presented, as far as a revocation decision
// needs it: who the certificate authority says it is, and which certificate
// said so.
//
// Serial and the validity window are empty for the development header path,
// which presents no certificate. That path is still subject to identity and
// Environment revocations -- an empty NotBefore is never after anything, so a
// revoked identity stays revoked whichever way it arrives.
type AgentCredential struct {
	Identity  AgentIdentity
	Serial    string
	NotBefore time.Time
	NotAfter  time.Time
}

// AgentCredentialFromCertificate reads the identity and the certificate facts
// a revocation is decided on. The certificate must already be verified; this
// reads it, it does not trust it.
func AgentCredentialFromCertificate(certificate *x509.Certificate) (AgentCredential, error) {
	identity, err := AgentIdentityFromCertificate(certificate)
	if err != nil {
		return AgentCredential{}, err
	}
	credential := AgentCredential{
		Identity: identity, NotBefore: certificate.NotBefore.UTC(), NotAfter: certificate.NotAfter.UTC(),
	}
	if certificate.SerialNumber != nil {
		credential.Serial = certificate.SerialNumber.Text(16)
	}
	return credential, nil
}

// The three things an operator can cut off.
//
// A serial revocation is exact: it refuses one certificate and nothing else,
// so the Agent's replacement certificate works the moment it is issued. It
// needs the serial, which an operator has only if they kept the certificate or
// the Hub saw it (GET .../agent-credentials reports the last one presented).
//
// An Agent revocation needs nothing but the name. It refuses every certificate
// for that identity whose NotBefore is at or before the revocation's NotBefore
// instant, which defaults to the moment of revocation. A certificate issued
// after that instant is admitted, so re-enrolment is "issue a new certificate"
// and needs no second operator action. Two things follow from deciding on a
// timestamp the issuer wrote, and both are stated in
// docs/operations/agent-enrolment.md rather than hidden: an issuer that
// backdates NotBefore, or whose clock runs behind the Hub's, produces a
// replacement that is still refused until the instant passes; and a
// replacement certificate issued for the SAME private key readmits whoever
// holds that key, so re-enrolment must rotate the key.
//
// An Environment revocation is the Agent rule applied to every Agent identity
// in the Environment.
const (
	RevocationSerial      = "SERIAL"
	RevocationAgent       = "AGENT"
	RevocationEnvironment = "ENVIRONMENT"
)

// MaximumRevocationsPerEnvironment bounds what one Agent request reads. A
// deny-list that grows without limit turns every lease poll into a table scan.
const MaximumRevocationsPerEnvironment = 512

// MaximumRevocationReasonBytes bounds the free text an operator attaches.
const MaximumRevocationReasonBytes = 512

var (
	// ErrAgentRevoked is the decision that a credential is authentic and is not
	// welcome. It is distinct from ErrUnauthorized on purpose: the certificate
	// verified, so "we do not know who this is" would be untrue.
	ErrAgentRevoked = errors.New("agent credential is revoked")
	// ErrRevocationInvalid is a revocation an operator could not have meant.
	ErrRevocationInvalid = errors.New("agent revocation is invalid")
	// ErrRevocationNotFound is a removal naming nothing.
	ErrRevocationNotFound = errors.New("agent revocation not found")
	// ErrRevocationLimit is an Environment already at its bound.
	ErrRevocationLimit = errors.New("agent revocation limit reached for this Environment")
)

// CONDUIT-INVENTED: identity.AgentCredentialRevocation
// Displaced-by: a published or sanitized Apollo agent credential revocation record defining its scope, identifier and re-enrolment semantics.
type AgentRevocation struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Environment string `json:"environment"`
	AgentID     string `json:"agentId,omitempty"`
	Serial      string `json:"serial,omitempty"`
	// NotBefore is the latest certificate NotBefore this revocation refuses.
	// A certificate issued after it is admitted. Unused by a serial
	// revocation, which names its certificate exactly.
	NotBefore time.Time `json:"notBefore,omitzero"`
	Reason    string    `json:"reason"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// NewAgentRevocation validates an operator's intent and gives it the identity
// it is stored under.
//
// The identifier is derived from what is revoked, not minted at random, so
// revoking the same thing twice is one record whose instant and reason are
// replaced rather than two records that must both be found and removed.
func NewAgentRevocation(draft AgentRevocation, actor string, now time.Time) (AgentRevocation, error) {
	invalid := func(message string) (AgentRevocation, error) {
		return AgentRevocation{}, fmt.Errorf("%w: %s", ErrRevocationInvalid, message)
	}
	revocation := AgentRevocation{
		Scope:       strings.ToUpper(strings.TrimSpace(draft.Scope)),
		Environment: strings.TrimSpace(draft.Environment),
		AgentID:     strings.TrimSpace(draft.AgentID),
		Reason:      strings.TrimSpace(draft.Reason),
		CreatedBy:   strings.TrimSpace(actor),
		// Microseconds, because that is what the durable store keeps: a record
		// must read back equal to the one that was written.
		CreatedAt: now.UTC().Truncate(time.Microsecond),
	}
	if !DNSLabelPattern.MatchString(revocation.Environment) {
		return invalid("environment must be a DNS label")
	}
	if revocation.Reason == "" || len(revocation.Reason) > MaximumRevocationReasonBytes {
		return invalid(fmt.Sprintf("reason is required and at most %d bytes", MaximumRevocationReasonBytes))
	}
	if revocation.CreatedBy == "" {
		return invalid("an acting operator is required")
	}
	switch revocation.Scope {
	case RevocationSerial:
		serial, err := NormalizeCertificateSerial(draft.Serial)
		if err != nil {
			return invalid(err.Error())
		}
		if revocation.AgentID != "" || !draft.NotBefore.IsZero() {
			return invalid("a serial revocation names one certificate; agentId and notBefore do not apply")
		}
		revocation.Serial = serial
	case RevocationAgent:
		if !DNSLabelPattern.MatchString(revocation.AgentID) {
			return invalid("agentId must be a DNS label")
		}
		if strings.TrimSpace(draft.Serial) != "" {
			return invalid("an Agent revocation does not take a serial; use scope SERIAL")
		}
	case RevocationEnvironment:
		if revocation.AgentID != "" || strings.TrimSpace(draft.Serial) != "" {
			return invalid("an Environment revocation takes neither agentId nor serial")
		}
	default:
		return invalid("scope must be SERIAL, AGENT or ENVIRONMENT")
	}
	if revocation.Scope != RevocationSerial {
		revocation.NotBefore = draft.NotBefore.UTC().Truncate(time.Microsecond)
		if draft.NotBefore.IsZero() {
			revocation.NotBefore = revocation.CreatedAt
		}
	}
	identity := sha256.Sum256([]byte(
		revocation.Scope + "\x00" + revocation.Environment + "\x00" + revocation.AgentID + "\x00" + revocation.Serial))
	revocation.ID = "arv_" + hex.EncodeToString(identity[:16])
	return revocation, nil
}

// NormalizeCertificateSerial reads a serial the way operators have it --
// `openssl x509 -noout -serial` prints upper-case hexadecimal, some tools
// separate octets with colons -- and returns the one spelling the Hub compares.
func NormalizeCertificateSerial(serial string) (string, error) {
	cleaned := strings.ToLower(strings.TrimSpace(serial))
	cleaned = strings.TrimPrefix(cleaned, "serial=")
	cleaned = strings.TrimPrefix(cleaned, "0x")
	cleaned = strings.ReplaceAll(cleaned, ":", "")
	if cleaned == "" || len(cleaned) > 64 {
		return "", errors.New("serial must be 1 to 64 hexadecimal digits")
	}
	value, ok := new(big.Int).SetString(cleaned, 16)
	if !ok || value.Sign() < 0 {
		return "", errors.New("serial must be hexadecimal, as `openssl x509 -noout -serial` prints it")
	}
	return value.Text(16), nil
}

// Refuses reports whether this revocation cuts the credential off.
//
// A record whose scope this code does not know refuses everything in its
// Environment. A deny-list entry that is not understood must not read as
// "nothing is denied".
func (revocation AgentRevocation) Refuses(credential AgentCredential) bool {
	if revocation.Environment != credential.Identity.Environment {
		return false
	}
	switch revocation.Scope {
	case RevocationSerial:
		return credential.Serial != "" && credential.Serial == revocation.Serial
	case RevocationAgent:
		if revocation.AgentID != credential.Identity.AgentID {
			return false
		}
	case RevocationEnvironment:
	default:
		return true
	}
	return !credential.NotBefore.After(revocation.NotBefore)
}

// RefusingRevocation returns the first revocation that cuts the credential
// off, so a refusal can say which record an operator would have to remove.
func RefusingRevocation(revocations []AgentRevocation, credential AgentCredential) (AgentRevocation, bool) {
	for _, revocation := range revocations {
		if revocation.Refuses(credential) {
			return revocation, true
		}
	}
	return AgentRevocation{}, false
}

// AgentCredentialSighting is the last certificate an Agent identity presented
// on an authenticated request. It exists so an operator can see an expiry
// coming and can find the serial of a certificate they need to revoke.
type AgentCredentialSighting struct {
	Environment string    `json:"environment"`
	AgentID     string    `json:"agentId"`
	Serial      string    `json:"serial"`
	NotBefore   time.Time `json:"notBefore"`
	NotAfter    time.Time `json:"notAfter"`
	SeenAt      time.Time `json:"lastSeenAt"`
}
