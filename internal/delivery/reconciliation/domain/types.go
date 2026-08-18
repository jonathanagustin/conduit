// Package domain owns Conduit's storage- and transport-neutral reconciliation
// vocabulary and deterministic convergence decision.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type DecisionKind string

const (
	DecisionConverged   DecisionKind = "CONVERGED"
	DecisionProposePlan DecisionKind = "PROPOSE_PLAN"
	DecisionBlocked     DecisionKind = "BLOCKED"
	DecisionWait        DecisionKind = "WAIT"
)

type Key struct {
	Environment  string `json:"environment"`
	Installation string `json:"installation"`
}

func (key Key) String() string {
	return key.Environment + "\x00" + key.Installation
}

func (key Key) Validate() error {
	if strings.TrimSpace(key.Environment) == "" || strings.TrimSpace(key.Installation) == "" {
		return errors.New("reconciliation key requires Environment and installation")
	}
	if key.Environment != strings.TrimSpace(key.Environment) || key.Installation != strings.TrimSpace(key.Installation) {
		return errors.New("reconciliation key values must be canonical")
	}
	return nil
}

type DesiredSnapshot struct {
	Key           Key             `json:"key"`
	Generation    string          `json:"generation"`
	TargetDigest  string          `json:"targetDigest"`
	PayloadDigest string          `json:"payloadDigest"`
	Payload       json.RawMessage `json:"payload"`
	RecordedAt    time.Time       `json:"recordedAt"`
}

func (snapshot DesiredSnapshot) Validate() error {
	if err := snapshot.Key.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(snapshot.Generation) == "" || snapshot.Generation != strings.TrimSpace(snapshot.Generation) ||
		!digestValid(snapshot.TargetDigest) || !digestValid(snapshot.PayloadDigest) {
		return errors.New("desired snapshot requires a generation and lowercase sha256 digests")
	}
	if len(snapshot.Payload) == 0 || !json.Valid(snapshot.Payload) {
		return errors.New("desired snapshot payload must be valid JSON")
	}
	sum := sha256.Sum256(snapshot.Payload)
	if "sha256:"+hex.EncodeToString(sum[:]) != snapshot.PayloadDigest {
		return errors.New("desired snapshot payload digest does not match its bytes")
	}
	if snapshot.RecordedAt.IsZero() {
		return errors.New("desired snapshot requires recordedAt")
	}
	return nil
}

type Observation struct {
	Key          Key       `json:"key"`
	Generation   string    `json:"generation"`
	TargetDigest string    `json:"targetDigest"`
	Complete     bool      `json:"complete"`
	ObservedAt   time.Time `json:"observedAt"`
}

func (observation Observation) Validate() error {
	if err := observation.Key.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(observation.Generation) == "" || observation.Generation != strings.TrimSpace(observation.Generation) ||
		!digestValid(observation.TargetDigest) || observation.ObservedAt.IsZero() {
		return errors.New("observation requires a generation, lowercase sha256 target digest, and observedAt")
	}
	return nil
}

type Input struct {
	Desired     *DesiredSnapshot `json:"desired,omitempty"`
	Observation *Observation     `json:"observation,omitempty"`
}

func (input Input) Key() (Key, error) {
	if input.Desired != nil {
		if err := input.Desired.Validate(); err != nil {
			return Key{}, err
		}
		if input.Observation != nil && input.Observation.Key != input.Desired.Key {
			return Key{}, errors.New("desired and observed reconciliation keys differ")
		}
		return input.Desired.Key, nil
	}
	if input.Observation != nil {
		if err := input.Observation.Validate(); err != nil {
			return Key{}, err
		}
		return input.Observation.Key, nil
	}
	return Key{}, errors.New("reconciliation input is empty")
}

func (input Input) Digest() (string, error) {
	if _, err := input.Key(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode reconciliation input: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type Decision struct {
	ID          string       `json:"id"`
	Key         Key          `json:"key"`
	InputDigest string       `json:"inputDigest"`
	Kind        DecisionKind `json:"kind"`
	Reason      string       `json:"reason"`
	Attempt     uint32       `json:"attempt"`
	DecidedAt   time.Time    `json:"decidedAt"`
	WakeAt      *time.Time   `json:"wakeAt,omitempty"`
}

type Proposal struct {
	ID             string          `json:"id"`
	Key            Key             `json:"key"`
	InputDigest    string          `json:"inputDigest"`
	IdempotencyKey string          `json:"idempotencyKey"`
	Desired        DesiredSnapshot `json:"desired"`
	// ObservedAt is the instant of the observation that made this proposal
	// necessary, and it travels with the proposal because a dispatcher cannot
	// otherwise tell two opposite situations apart: an installation that has
	// drifted back after a Plan already succeeded, and a report that was taken
	// before that Plan finished and has simply not caught up. Both reach dispatch
	// as "desired and reported state differ" against a SUCCEEDED Plan; only the
	// timestamps separate them. It is zero when the decision had no observation
	// at all, which is the same evidence a proposal carried before this field
	// existed, so a row written by an older writer stays readable and is treated
	// as the case that cannot be distinguished.
	ObservedAt time.Time `json:"observedAt,omitzero"`
}

type PlanReceipt struct {
	ProposalID string    `json:"proposalId"`
	PlanID     string    `json:"planId"`
	CreatedAt  time.Time `json:"createdAt"`
}

func Decide(input Input, attempt uint32, now time.Time, maximumObservationAge time.Duration) (Decision, *Proposal, error) {
	key, err := input.Key()
	if err != nil {
		return Decision{}, nil, err
	}
	inputDigest, err := input.Digest()
	if err != nil {
		return Decision{}, nil, err
	}
	decision := Decision{
		ID: stableID("rcd_", key.String()+"\x00"+inputDigest), Key: key, InputDigest: inputDigest,
		Attempt: attempt, DecidedAt: now.UTC(),
	}
	if input.Desired == nil {
		decision.Kind, decision.Reason = DecisionBlocked, "MISSING_DESIRED_STATE"
		return decision, nil, nil
	}
	if input.Observation != nil {
		if err := input.Observation.Validate(); err != nil {
			return Decision{}, nil, err
		}
		if maximumObservationAge > 0 && now.UTC().Sub(input.Observation.ObservedAt.UTC()) > maximumObservationAge {
			decision.Kind, decision.Reason = DecisionBlocked, "STALE_OBSERVATION"
			return decision, nil, nil
		}
		if input.Observation.Complete && input.Observation.Generation == input.Desired.Generation &&
			input.Observation.TargetDigest == input.Desired.TargetDigest {
			decision.Kind, decision.Reason = DecisionConverged, "EXACT_REPORTED_STATE"
			return decision, nil, nil
		}
	}
	decision.Kind, decision.Reason = DecisionProposePlan, "DESIRED_AND_REPORTED_STATE_DIFFER"
	proposal := &Proposal{
		ID: stableID("rcp_", decision.ID), Key: key, InputDigest: inputDigest,
		IdempotencyKey: stableID("rci_", inputDigest+"\x00"+input.Desired.PayloadDigest), Desired: *input.Desired,
	}
	if input.Observation != nil {
		proposal.ObservedAt = input.Observation.ObservedAt.UTC()
	}
	return decision, proposal, nil
}

func stableID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(sum[:16])
}

func digestValid(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && value == strings.ToLower(value)
}
