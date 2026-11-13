package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
