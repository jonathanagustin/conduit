package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization/solverprotocol"
)

const EvidenceAPIVersion = solverprotocol.EvidenceAPIVersion

type Snapshot struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Revision   string `json:"revision"`
	CapturedAt string `json:"capturedAt"`
}

type Objective struct {
	Primary   string   `json:"primary"`
	Secondary []string `json:"secondary,omitempty"`
}

type ConstraintEvidence struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Revision   string `json:"revision"`
	ObservedAt string `json:"observedAt"`
}

type RecommendationRequest struct {
	APIVersion  string               `json:"apiVersion"`
	Kind        string               `json:"kind"`
	Snapshot    Snapshot             `json:"snapshot"`
	Objective   Objective            `json:"objective"`
	Constraints []ConstraintEvidence `json:"constraints"`
	Problem     Problem              `json:"problem"`
}

type Recommendation struct {
	APIVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	ID         string                `json:"id"`
	CreatedAt  string                `json:"createdAt"`
	Request    RecommendationRequest `json:"request"`
	Result     Result                `json:"result"`
}

// RecommendationSnapshot creates the only supported bridge from a bitemporal
// twin snapshot into solver evidence. Incomplete snapshots cannot be promoted
// into apparently complete recommendation inputs.
func RecommendationSnapshot(twin TwinSnapshot, source, revision string) (Snapshot, error) {
	if err := VerifyTwinSnapshot(twin); err != nil {
		return Snapshot{}, err
	}
	if !twin.Complete {
		return Snapshot{}, errors.New("incomplete twin snapshot cannot back a recommendation")
	}
	if source == "" || revision == "" {
		return Snapshot{}, errors.New("snapshot reference source and revision are required")
	}
	return Snapshot{ID: twin.ID, Source: source, Revision: revision, CapturedAt: twin.KnownAt}, nil
}

// Recommend produces reproducible advisory evidence. The identifier commits to
// the complete input and result but excludes CreatedAt, so replaying the same
// snapshot and solver produces the same identity.
func Recommend(request RecommendationRequest, now time.Time) (Recommendation, error) {
	if err := validateRecommendationRequest(request); err != nil {
		return Recommendation{}, err
	}
	request = canonicalRequest(request)
	result, err := solve(request.Problem, hasObjective(request.Objective.Secondary, "minimize_estimated_cost"))
	if err != nil {
		return Recommendation{}, err
	}
	payload := struct {
		Request RecommendationRequest `json:"request"`
		Result  Result                `json:"result"`
	}{request, result}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Recommendation{}, fmt.Errorf("encode recommendation identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return Recommendation{APIVersion: EvidenceAPIVersion, Kind: "Recommendation", ID: "rec_" + hex.EncodeToString(digest[:]), CreatedAt: now.UTC().Format(time.RFC3339Nano), Request: request, Result: result}, nil
}

// VerifyRecommendation independently re-solves and re-hashes an advisory
// document before downstream outcome evidence may rely on it.
func VerifyRecommendation(recommendation Recommendation) error {
	if recommendation.APIVersion != EvidenceAPIVersion || recommendation.Kind != "Recommendation" {
		return errors.New("unsupported recommendation type")
	}
	created, err := time.Parse(time.RFC3339Nano, recommendation.CreatedAt)
	if err != nil {
		return fmt.Errorf("recommendation createdAt: %w", err)
	}
	captured, err := time.Parse(time.RFC3339, recommendation.Request.Snapshot.CapturedAt)
	if err != nil {
		return fmt.Errorf("snapshot capturedAt: %w", err)
	}
	if created.Before(captured) {
		return errors.New("recommendation predates its snapshot")
	}
	replayed, err := Recommend(recommendation.Request, created)
	if err != nil {
		return fmt.Errorf("replay recommendation: %w", err)
	}
	if replayed.ID != recommendation.ID {
		return errors.New("recommendation identity does not match content")
	}
	if !reflect.DeepEqual(replayed.Result, recommendation.Result) {
		return errors.New("recommendation result does not match replay")
	}
	return nil
}

func validateRecommendationRequest(request RecommendationRequest) error {
	if request.APIVersion != EvidenceAPIVersion || request.Kind != "RecommendationRequest" {
		return errors.New("unsupported recommendation request type")
	}
	if request.Snapshot.ID == "" || request.Snapshot.Source == "" || request.Snapshot.Revision == "" {
		return errors.New("snapshot id, source, and revision are required")
	}
	if !validContentID(request.Snapshot.ID, "snap_") {
		return errors.New("snapshot id must be a content-addressed snap_ digest")
	}
	captured, err := time.Parse(time.RFC3339, request.Snapshot.CapturedAt)
	if err != nil {
		return fmt.Errorf("snapshot capturedAt must be RFC3339: %w", err)
	}
	if request.Objective.Primary != "minimize_makespan" {
		return fmt.Errorf("unsupported primary objective %q", request.Objective.Primary)
	}
	allowedSecondary := map[string]bool{"minimize_estimated_cost": true}
	seenSecondary := make(map[string]bool, len(request.Objective.Secondary))
	for _, objective := range request.Objective.Secondary {
		if !allowedSecondary[objective] {
			return fmt.Errorf("unsupported secondary objective %q", objective)
		}
		if seenSecondary[objective] {
			return fmt.Errorf("duplicate secondary objective %q", objective)
		}
		seenSecondary[objective] = true
	}
	constraints := append([]ConstraintEvidence(nil), request.Constraints...)
	sort.Slice(constraints, func(i, j int) bool { return constraints[i].ID < constraints[j].ID })
	for i, constraint := range constraints {
		if constraint.ID == "" || constraint.Source == "" || constraint.Revision == "" {
			return errors.New("constraint id, source, and revision are required")
		}
		if i > 0 && constraints[i-1].ID == constraint.ID {
			return fmt.Errorf("duplicate constraint %q", constraint.ID)
		}
		observed, parseErr := time.Parse(time.RFC3339, constraint.ObservedAt)
		if parseErr != nil {
			return fmt.Errorf("constraint %q observedAt must be RFC3339: %w", constraint.ID, parseErr)
		}
		if observed.After(captured) {
			return fmt.Errorf("constraint %q is newer than snapshot", constraint.ID)
		}
	}
	return nil
}

func hasObjective(objectives []string, want string) bool {
	for _, objective := range objectives {
		if objective == want {
			return true
		}
	}
	return false
}

func canonicalRequest(request RecommendationRequest) RecommendationRequest {
	request.Objective.Secondary = append([]string(nil), request.Objective.Secondary...)
	sort.Strings(request.Objective.Secondary)
	request.Constraints = append([]ConstraintEvidence(nil), request.Constraints...)
	sort.Slice(request.Constraints, func(i, j int) bool { return request.Constraints[i].ID < request.Constraints[j].ID })
	request.Problem.Items = append([]WorkItem(nil), request.Problem.Items...)
	sort.Slice(request.Problem.Items, func(i, j int) bool { return request.Problem.Items[i].ID < request.Problem.Items[j].ID })
	for i := range request.Problem.Items {
		request.Problem.Items[i].Eligible = append([]string(nil), request.Problem.Items[i].Eligible...)
		sort.Strings(request.Problem.Items[i].Eligible)
	}
	request.Problem.Resources = append([]Resource(nil), request.Problem.Resources...)
	sort.Slice(request.Problem.Resources, func(i, j int) bool { return request.Problem.Resources[i].ID < request.Problem.Resources[j].ID })
	return request
}
