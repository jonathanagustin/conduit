package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// AuditChainVersion identifies Conduit's audit-integrity format. It is not an
// Apollo audit transport or a signature asserting the truth of an event.
const AuditChainVersion = "conduit.authorization-audit-chain/v1"
const AuditOriginLive = "LIVE"
const AuditOriginLegacy = "LEGACY_IMPORT"

// AuditCheckpoint must be retained independently of the database to detect a
// database owner rewriting an otherwise internally consistent chain.
type AuditCheckpoint struct {
	Version  string `json:"version"`
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

// AuditChainRecord binds the exact stored event bytes and lookup coordinates
// to their predecessor. Legacy imports attest only the bytes present at cutover.
type AuditChainRecord struct {
	Sequence       uint64
	PreviousDigest string
	EventID        string
	ResourceRID    string
	Origin         string
	Payload        []byte
	Digest         string
}

// AuthorizationAuditChainEntry is one recorded decision bound to the hash of
// the entry before it. The digest is the audit-chain digest of that event.
type AuthorizationAuditChainEntry struct {
	Sequence       uint64    `json:"sequence"`
	ID             string    `json:"id"`
	Actor          string    `json:"actor"`
	Action         string    `json:"action"`
	Decision       string    `json:"decision"`
	PreviousDigest string    `json:"previousDigest"`
	Digest         string    `json:"digest"`
	CreatedAt      time.Time `json:"createdAt"`
	// Context carries recorded model-exchange fields when present, including
	// promptSha256, responseSha256, a packaging skill hash, and tool names.
	// It is display data; it is not an input to the chain digest.
	Context map[string]any `json:"context,omitempty"`
	// These are the exact inputs used for this returned projection's digest.
	// They do not expose the persisted ledger's commit order or trust anchor.
	ResourceRID string `json:"resourceRid"`
	Origin      string `json:"origin"`
	Payload     []byte `json:"payload"`
}

// ChainAuthorizationAudit folds recorded events, oldest first, into the same
// hash chain an append uses. Each previous digest is the digest of the entry
// before it, and the first previous digest is the genesis checkpoint.
func ChainAuthorizationAudit(events []AuthorizationAuditEvent) ([]AuthorizationAuditChainEntry, error) {
	ordered := append([]AuthorizationAuditEvent(nil), events...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	return ChainAuthorizationAuditFrom(EmptyAuditCheckpoint(), ordered)
}

// ChainAuthorizationAuditFrom continues the projection after a predecessor
// checkpoint, in the order given. A paginated read folds each page from the
// checkpoint its previous page ended on, so concatenated pages equal the fold
// of the whole read. The caller owns the order; this function does not sort.
func ChainAuthorizationAuditFrom(previous AuditCheckpoint, ordered []AuthorizationAuditEvent) ([]AuthorizationAuditChainEntry, error) {
	entries := make([]AuthorizationAuditChainEntry, 0, len(ordered))
	for _, event := range ordered {
		record, err := NewAuditChainRecord(previous, event, AuditOriginLive)
		if err != nil {
			return nil, err
		}
		entries = append(entries, AuthorizationAuditChainEntry{
			Sequence: record.Sequence, ID: event.ID, Actor: event.Subject, Action: event.Action,
			Decision: event.Decision, PreviousDigest: record.PreviousDigest, Digest: record.Digest,
			CreatedAt: event.CreatedAt, Context: event.Context,
			ResourceRID: record.ResourceRID, Origin: record.Origin, Payload: record.Payload,
		})
		previous = AuditCheckpoint{Version: AuditChainVersion, Sequence: record.Sequence, Digest: record.Digest}
	}
	return entries, nil
}

func EmptyAuditCheckpoint() AuditCheckpoint {
	digest := sha256.Sum256([]byte(AuditChainVersion + ":genesis"))
	return AuditCheckpoint{Version: AuditChainVersion, Digest: hex.EncodeToString(digest[:])}
}

func (checkpoint AuditCheckpoint) Validate() error {
	decoded, err := hex.DecodeString(checkpoint.Digest)
	if checkpoint.Version != AuditChainVersion || err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != checkpoint.Digest || checkpoint.Sequence > math.MaxInt64 {
		return errors.New("invalid authorization audit checkpoint")
	}
	if checkpoint.Sequence == 0 && checkpoint != EmptyAuditCheckpoint() {
		return errors.New("invalid authorization audit genesis checkpoint")
	}
	return nil
}

func NewAuditChainRecord(previous AuditCheckpoint, event AuthorizationAuditEvent, origin string) (AuditChainRecord, error) {
	if err := previous.Validate(); err != nil {
		return AuditChainRecord{}, err
	}
	if previous.Sequence == math.MaxInt64 {
		return AuditChainRecord{}, errors.New("authorization audit sequence exhausted")
	}
	if event.ID == "" {
		return AuditChainRecord{}, errors.New("authorization audit event ID is required")
	}
	if err := ValidateAuditCategoryEvent(event); err != nil {
		return AuditChainRecord{}, err
	}
	if origin != AuditOriginLive && origin != AuditOriginLegacy {
		return AuditChainRecord{}, errors.New("invalid authorization audit origin")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return AuditChainRecord{}, errors.New("encode authorization audit event")
	}
	record := AuditChainRecord{Sequence: previous.Sequence + 1, PreviousDigest: previous.Digest, EventID: event.ID, ResourceRID: event.ResourceRID, Origin: origin, Payload: payload}
	record.Digest = auditRecordDigest(record)
	return record, nil
}

// VerifyAuditChainRecord checks both chain continuity and the indexed event
// identity. This proves consistency; an independent checkpoint supplies the
// additional trust anchor needed to detect a coherently rewritten prefix.
func VerifyAuditChainRecord(previous AuditCheckpoint, record AuditChainRecord) (AuditCheckpoint, error) {
	if err := previous.Validate(); err != nil {
		return AuditCheckpoint{}, err
	}
	if previous.Sequence == math.MaxInt64 || record.Sequence != previous.Sequence+1 || record.PreviousDigest != previous.Digest {
		return AuditCheckpoint{}, errors.New("authorization audit chain discontinuity")
	}
	if record.Origin != AuditOriginLive && record.Origin != AuditOriginLegacy {
		return AuditCheckpoint{}, errors.New("invalid authorization audit origin")
	}
	var event AuthorizationAuditEvent
	if err := json.Unmarshal(record.Payload, &event); err != nil {
		return AuditCheckpoint{}, errors.New("invalid authorization audit payload")
	}
	if record.EventID == "" || event.ID != record.EventID || event.ResourceRID != record.ResourceRID {
		return AuditCheckpoint{}, errors.New("authorization audit index does not match its payload")
	}
	if err := ValidateAuditCategoryEvent(event); err != nil {
		return AuditCheckpoint{}, err
	}
	if record.Digest != auditRecordDigest(record) {
		return AuditCheckpoint{}, fmt.Errorf("authorization audit digest mismatch at sequence %d", record.Sequence)
	}
	return AuditCheckpoint{Version: AuditChainVersion, Sequence: record.Sequence, Digest: record.Digest}, nil
}

func auditRecordDigest(record AuditChainRecord) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(AuditChainVersion))
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], record.Sequence)
	_, _ = hasher.Write(encoded[:])
	// Length framing distinguishes concatenations without depending on JSON
	// field ordering or a future re-serialization of the payload.
	for _, value := range [][]byte{[]byte(record.PreviousDigest), []byte(record.EventID), []byte(record.ResourceRID), []byte(record.Origin), record.Payload} {
		binary.BigEndian.PutUint64(encoded[:], uint64(len(value)))
		_, _ = hasher.Write(encoded[:])
		_, _ = hasher.Write(value)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
