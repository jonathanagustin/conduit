// Package domain describes committed-event hints and identity-scoped delivery state.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	Version            = 1
	MaxSequence        = uint64(9007199254740991)
	MaxEnvelopeBytes   = 4096
	MaxQueueEvents     = 256
	MaxQueueBytes      = 1024 * 1024
	MaxReplayEvents    = 10000
	ReplayRetention    = 10 * time.Minute
	HeartbeatInterval  = 15 * time.Second
	ConnectionLifetime = 30 * time.Minute
)

var (
	ErrUnauthorized = errors.New("operator identity required")
	ErrForbidden    = errors.New("event stream permission required")
	ErrReset        = errors.New("event stream reset required")
	// ErrSourceRetentionGap is a trusted source's explicit truncation signal.
	// Other reset/corruption errors must not erase durable producer checkpoints.
	ErrSourceRetentionGap = fmt.Errorf("%w: committed source retention gap", ErrReset)
	ErrUnavailable        = errors.New("event delivery unavailable")
	ErrConflict           = errors.New("committed event identity conflict")
	ErrInvalid            = errors.New("invalid event contract")
	ErrBusy               = errors.New("previous delivery is not acknowledged")
	ErrClosed             = errors.New("event subscription closed")
)

// Identity contains only hashes derived by a trusted current native authorizer.
// A caller's request must never supply these fields.
type Identity struct {
	Owner         string `json:"owner"`
	Represented   string `json:"represented"`
	Authorization string `json:"authorization"`
}

func (identity Identity) Valid() bool {
	for _, value := range []string{identity.Owner, identity.Represented, identity.Authorization} {
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != value {
			return false
		}
	}
	return true
}
func (identity Identity) Key() string {
	body, _ := json.Marshal(identity)
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

// Subject is a private read-model authorization coordinate, never wire data.
type Subject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Resource identifies the persisted owning read model and its native revision.
type Resource struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}

// Content is an invalidation/progress hint, not approval or execution authority.
type Content struct {
	OccurredAt time.Time       `json:"occurredAt"`
	Kind       string          `json:"kind"`
	Resource   Resource        `json:"resource"`
	Data       json.RawMessage `json:"data"`
}

// Record must be constructed from a committed native transition by an adapter.
// There is no public operation accepting arbitrary event JSON.
type Record struct {
	Producer string  `json:"producer"`
	Position uint64  `json:"position"`
	ID       string  `json:"id"`
	Subject  Subject `json:"subject"`
	Content  Content `json:"content"`
}

// Envelope is the exact versioned wire contract after visibility filtering.
type Envelope struct {
	Version  int    `json:"version"`
	StreamID string `json:"streamID"`
	Sequence uint64 `json:"sequence"`
	Cursor   string `json:"cursor"`
	Content
}

// Frame keeps private scope beside its immutable public replay envelope.
type Frame struct {
	Envelope Envelope  `json:"envelope"`
	Subject  Subject   `json:"subject"`
	StoredAt time.Time `json:"storedAt"`
}

func (frame Frame) Encoded() ([]byte, error) { return json.Marshal(frame.Envelope) }

// WireSize includes the bounded SSE id/event/data line framing.
func (frame Frame) WireSize() int {
	body, _ := frame.Encoded()
	return len(body) + len(frame.Envelope.Cursor) + len(frame.Envelope.Kind) + 21
}

type SourceRow struct {
	Position uint64    `json:"position"`
	StoredAt time.Time `json:"storedAt"`
	Record   Record    `json:"record"`
}
type Stream struct {
	ID             string    `json:"id"`
	Identity       Identity  `json:"identity"`
	CreatedAt      time.Time `json:"createdAt"`
	RootCursor     string    `json:"rootCursor"`
	SourcePosition uint64    `json:"sourcePosition"`
	LastSequence   uint64    `json:"lastSequence"`
	Frames         []Frame   `json:"frames"`
}

// ProducerCheckpoint retains the durable native-source high watermark.
type ProducerCheckpoint struct {
	Position uint64 `json:"position"`
	Digest   string `json:"digest"`
}

// State is private journal state; none of its raw positions are public cursors.
type State struct {
	Producers      map[string]ProducerCheckpoint `json:"producers"`
	Version        int                           `json:"version"`
	SourcePosition uint64                        `json:"sourcePosition"`
	Sources        []SourceRow                   `json:"sources"`
	Streams        map[string]Stream             `json:"streams"`
}

func NewState() State {
	return State{Version: Version, Streams: map[string]Stream{}, Producers: map[string]ProducerCheckpoint{}}
}
func Clone(state State) (State, error) {
	body, err := json.Marshal(state)
	if err != nil {
		return State{}, err
	}
	var clone State
	err = json.Unmarshal(body, &clone)
	return clone, err
}

// ProducerID binds a private native source sequence to its owning read model.
func ProducerID(subject Subject) string {
	body, _ := json.Marshal(subject)
	hash := sha256.Sum256(body)
	return "p_" + hex.EncodeToString(hash[:])
}
func RecordID(producer string, position uint64) string {
	body, _ := json.Marshal([]any{producer, position})
	hash := sha256.Sum256(body)
	return "e_" + hex.EncodeToString(hash[:])
}

// IndexID identifies a derived per-resource invalidation without a global counter.
func IndexID(subject Subject) string {
	body, _ := json.Marshal(subject)
	hash := sha256.Sum256(body)
	return "idx_" + hex.EncodeToString(hash[:])
}
