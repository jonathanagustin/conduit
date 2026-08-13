package optimization

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type KnowledgeStatus string

const (
	KnowledgeKnown         KnowledgeStatus = "KNOWN"
	KnowledgeUnknown       KnowledgeStatus = "UNKNOWN"
	KnowledgeStale         KnowledgeStatus = "STALE"
	KnowledgeConflicting   KnowledgeStatus = "CONFLICTING"
	KnowledgeNotApplicable KnowledgeStatus = "NOT_APPLICABLE"
)

type TemporalFact struct {
	ID                string          `json:"id"`
	EntityID          string          `json:"entityId"`
	Attribute         string          `json:"attribute"`
	Value             json.RawMessage `json:"value"`
	Unit              string          `json:"unit"`
	Plane             StatePlane      `json:"plane"`
	KnowledgeStatus   KnowledgeStatus `json:"knowledgeStatus"`
	ValidFrom         string          `json:"validFrom"`
	ValidUntil        string          `json:"validUntil,omitempty"`
	RecordedAt        string          `json:"recordedAt"`
	Source            string          `json:"sourceCall"`
	SourceRevision    string          `json:"sourceRevision"`
	RequestDigest     string          `json:"requestDigest"`
	ResponseDigest    string          `json:"responseDigest"`
	CollectorIdentity string          `json:"collectorIdentity"`
	Fidelity          Fidelity        `json:"fidelity"`
	Uncertainty       string          `json:"uncertainty,omitempty"`
}

type TwinRequirement struct {
	ID                      string     `json:"id"`
	EntityID                string     `json:"entityId"`
	Attribute               string     `json:"attribute"`
	Unit                    string     `json:"unit"`
	Plane                   StatePlane `json:"plane"`
	MinimumFidelity         Fidelity   `json:"minimumFidelity"`
	MaximumStalenessSeconds int64      `json:"maximumStalenessSeconds"`
	Applicable              *bool      `json:"applicable,omitempty"`
}

type SnapshotField struct {
	RequirementID string          `json:"requirementId"`
	Status        KnowledgeStatus `json:"status"`
	FactIDs       []string        `json:"factIds,omitempty"`
	Value         json.RawMessage `json:"value,omitempty"`
	Unit          string          `json:"unit"`
	Reason        string          `json:"reason,omitempty"`
}

type TwinSnapshot struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	ID         string          `json:"id"`
	ValidAt    string          `json:"validAt"`
	KnownAt    string          `json:"knownAt"`
	Complete   bool            `json:"complete"`
	Fields     []SnapshotField `json:"fields"`
}

// BuildTwinSnapshot performs an as-of bitemporal query: ValidAt selects when a
// fact held in reality; KnownAt selects only facts recorded by that knowledge
// time. It preserves conflicts and stale/unknown state rather than choosing a
// convenient source.
func BuildTwinSnapshot(requirements []TwinRequirement, facts []TemporalFact, validAt, knownAt time.Time) (TwinSnapshot, error) {
	if validAt.IsZero() || knownAt.IsZero() {
		return TwinSnapshot{}, errors.New("valid and knowledge times are required")
	}
	requirements = append([]TwinRequirement(nil), requirements...)
	facts = append([]TemporalFact(nil), facts...)
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	seenRequirements := map[string]bool{}
	for _, requirement := range requirements {
		if requirement.ID == "" || requirement.EntityID == "" || requirement.Attribute == "" || requirement.Unit == "" || requirement.MaximumStalenessSeconds < 0 || requirement.MaximumStalenessSeconds > int64((1<<63-1)/int64(time.Second)) || fidelityRank(requirement.MinimumFidelity) == 0 {
			return TwinSnapshot{}, fmt.Errorf("invalid twin requirement %q", requirement.ID)
		}
		if seenRequirements[requirement.ID] {
			return TwinSnapshot{}, fmt.Errorf("duplicate twin requirement %q", requirement.ID)
		}
		seenRequirements[requirement.ID] = true
		if err := validateRequirement(StateRequirement{ID: requirement.ID, Plane: requirement.Plane, MinimumFidelity: requirement.MinimumFidelity}); err != nil {
			return TwinSnapshot{}, err
		}
	}
	for _, fact := range facts {
		if err := VerifyTemporalFact(fact); err != nil {
			return TwinSnapshot{}, err
		}
	}
	snapshot := TwinSnapshot{APIVersion: EvidenceAPIVersion, Kind: "TwinSnapshot", ValidAt: validAt.UTC().Format(time.RFC3339Nano), KnownAt: knownAt.UTC().Format(time.RFC3339Nano)}
	for _, requirement := range requirements {
		field := SnapshotField{RequirementID: requirement.ID, Status: KnowledgeUnknown, Unit: requirement.Unit, Reason: "no matching fact known at query time"}
		if requirement.Applicable != nil && !*requirement.Applicable {
			field.Status, field.Reason = KnowledgeNotApplicable, "declared not applicable by requirement contract"
			snapshot.Fields = append(snapshot.Fields, field)
			continue
		}
		matching := []TemporalFact{}
		stale := []TemporalFact{}
		for _, fact := range facts {
			if fact.EntityID != requirement.EntityID || fact.Attribute != requirement.Attribute || fact.Unit != requirement.Unit || fact.Plane != requirement.Plane || !fidelitySatisfies(requirement.MinimumFidelity, fact.Fidelity) {
				continue
			}
			recorded, _ := time.Parse(time.RFC3339Nano, fact.RecordedAt)
			from, _ := time.Parse(time.RFC3339Nano, fact.ValidFrom)
			if recorded.After(knownAt) || from.After(validAt) {
				continue
			}
			if fact.ValidUntil != "" {
				until, _ := time.Parse(time.RFC3339Nano, fact.ValidUntil)
				if !validAt.Before(until) {
					continue
				}
			}
			if validAt.Sub(from) > time.Duration(requirement.MaximumStalenessSeconds)*time.Second {
				stale = append(stale, fact)
				continue
			}
			matching = append(matching, fact)
		}
		if len(matching) == 0 && len(stale) > 0 {
			field.Status = KnowledgeStale
			field.Reason = "matching facts exceed maximum staleness"
			for _, fact := range stale {
				field.FactIDs = append(field.FactIDs, fact.ID)
			}
		}
		if len(matching) > 0 {
			sort.Slice(matching, func(i, j int) bool { return matching[i].ID < matching[j].ID })
			field.Status, field.Reason = KnowledgeKnown, ""
			field.Value = append(json.RawMessage(nil), matching[0].Value...)
			for _, fact := range matching {
				field.FactIDs = append(field.FactIDs, fact.ID)
				if string(fact.Value) != string(field.Value) {
					field.Status = KnowledgeConflicting
					field.Value = nil
					field.Reason = "authoritative facts disagree"
				}
			}
		}
		snapshot.Fields = append(snapshot.Fields, field)
	}
	snapshot.Complete = true
	for _, field := range snapshot.Fields {
		if field.Status != KnowledgeKnown && field.Status != KnowledgeNotApplicable {
			snapshot.Complete = false
		}
	}
	id, err := twinSnapshotID(snapshot)
	if err != nil {
		return TwinSnapshot{}, err
	}
	snapshot.ID = id
	return snapshot, nil
}

func VerifyTwinSnapshot(snapshot TwinSnapshot) error {
	if snapshot.APIVersion != EvidenceAPIVersion || snapshot.Kind != "TwinSnapshot" {
		return errors.New("unsupported twin snapshot type")
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.ValidAt); err != nil {
		return fmt.Errorf("snapshot validAt: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.KnownAt); err != nil {
		return fmt.Errorf("snapshot knownAt: %w", err)
	}
	for i, field := range snapshot.Fields {
		if field.RequirementID == "" || field.Unit == "" {
			return errors.New("snapshot fields require requirement and unit")
		}
		if i > 0 && snapshot.Fields[i-1].RequirementID >= field.RequirementID {
			return errors.New("snapshot fields must have unique canonical order")
		}
		switch field.Status {
		case KnowledgeKnown, KnowledgeUnknown, KnowledgeStale, KnowledgeConflicting, KnowledgeNotApplicable:
		default:
			return fmt.Errorf("field %q has invalid knowledge status", field.RequirementID)
		}
		if field.Status == KnowledgeKnown && (len(field.Value) == 0 || !json.Valid(field.Value) || len(field.FactIDs) == 0) {
			return fmt.Errorf("known field %q lacks value or fact", field.RequirementID)
		}
	}
	id, err := twinSnapshotID(snapshot)
	if err != nil {
		return err
	}
	if id != snapshot.ID {
		return errors.New("twin snapshot id does not match content")
	}
	return nil
}

func twinSnapshotID(snapshot TwinSnapshot) (string, error) {
	payload := struct {
		ValidAt  string          `json:"validAt"`
		KnownAt  string          `json:"knownAt"`
		Complete bool            `json:"complete"`
		Fields   []SnapshotField `json:"fields"`
	}{snapshot.ValidAt, snapshot.KnownAt, snapshot.Complete, snapshot.Fields}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "snap_" + hex.EncodeToString(digest[:]), nil
}

func NewTemporalFact(fact TemporalFact) (TemporalFact, error) {
	fact.ID = ""
	if fact.KnowledgeStatus == "" {
		fact.KnowledgeStatus = KnowledgeKnown
	}
	if fact.Uncertainty == "" && fact.Fidelity == FidelityExact {
		fact.Uncertainty = "none"
	}
	if err := validateTemporalFact(fact); err != nil {
		return TemporalFact{}, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, fact.Value); err != nil {
		return TemporalFact{}, err
	}
	fact.Value = append(json.RawMessage(nil), compact.Bytes()...)
	encoded, err := json.Marshal(fact)
	if err != nil {
		return TemporalFact{}, err
	}
	digest := sha256.Sum256(encoded)
	fact.ID = "fact_" + hex.EncodeToString(digest[:])
	return fact, nil
}

func VerifyTemporalFact(fact TemporalFact) error {
	want := fact.ID
	rebuilt, err := NewTemporalFact(fact)
	if err != nil {
		return err
	}
	if want != rebuilt.ID {
		return errors.New("temporal fact id does not match content")
	}
	return nil
}

func validateTemporalFact(fact TemporalFact) error {
	if fact.EntityID == "" || fact.Attribute == "" || fact.Unit == "" || fact.Source == "" || fact.SourceRevision == "" || fact.RequestDigest == "" || fact.ResponseDigest == "" || fact.CollectorIdentity == "" || len(fact.Value) == 0 || !json.Valid(fact.Value) {
		return errors.New("temporal fact requires value, identity, unit, source provenance, digests, and collector")
	}
	if fidelityRank(fact.Fidelity) == 0 {
		return errors.New("temporal fact has invalid fidelity")
	}
	if fact.KnowledgeStatus != KnowledgeKnown {
		return errors.New("an observed temporal fact must have KNOWN status; query results represent other knowledge states")
	}
	if fact.Uncertainty == "" {
		return errors.New("non-exact temporal fact requires explicit uncertainty")
	}
	if err := validateRequirement(StateRequirement{ID: fact.Attribute, Plane: fact.Plane, MinimumFidelity: fact.Fidelity}); err != nil {
		return err
	}
	from, err := time.Parse(time.RFC3339Nano, fact.ValidFrom)
	if err != nil {
		return fmt.Errorf("validFrom: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, fact.RecordedAt); err != nil {
		return fmt.Errorf("recordedAt: %w", err)
	}
	if fact.ValidUntil != "" {
		until, err := time.Parse(time.RFC3339Nano, fact.ValidUntil)
		if err != nil || !until.After(from) {
			return errors.New("validUntil must be RFC3339 and after validFrom")
		}
	}
	return nil
}
