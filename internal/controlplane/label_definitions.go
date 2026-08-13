package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	LabelDefinitionActive              = "ACTIVE"
	LabelDefinitionTombstoned          = "TOMBSTONED"
	LabelDefinitionChangeTarget        = "LABEL_DEFINITION"
	ChangeTypeCreateLabelDefinition    = "CREATE_LABEL_DEFINITION"
	ChangeTypeUpdateLabelDefinition    = "UPDATE_LABEL_DEFINITION"
	ChangeTypeTombstoneLabelDefinition = "TOMBSTONE_LABEL_DEFINITION"
)

var labelIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

type LabelDefinitionRepository interface {
	PutLabelDefinition(context.Context, LabelDefinition) (LabelDefinition, error)
	GetLabelDefinition(context.Context, string) (LabelDefinition, error)
	ListLabelDefinitions(context.Context, bool) ([]LabelDefinition, error)
	ListLabelDefinitionRevisions(context.Context, string) ([]LabelDefinition, error)
}

// LabelDefinitionApplicationRepository is the outbound port used by the
// label-definition application service. It deliberately excludes unrelated
// control-plane persistence capabilities.
type LabelDefinitionApplicationRepository interface {
	LabelDefinitionRepository
	UpdateChangeRequest(context.Context, string, func(*ChangeRequest) error) (ChangeRequest, error)
}

// LabelDefinition adapts the public Apollo label-administration semantics
// without claiming its private GraphQL or persistence protocol.
// Public source reviewed 2026-08-02:
// https://www.palantir.com/docs/apollo/managing-labels/create-labels/
type LabelDefinition struct {
	ID                     string    `json:"id"`
	Description            string    `json:"description"`
	SuggestedResourceTypes []string  `json:"suggestedResourceTypes"`
	AcceptedValues         []string  `json:"acceptedValues"`
	Status                 string    `json:"status"`
	Rationale              string    `json:"rationale"`
	CreatedBy              string    `json:"createdBy"`
	UpdatedBy              string    `json:"updatedBy"`
	ChangeRequestID        string    `json:"changeRequestId"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
	Revision               uint64    `json:"revision"`
}

type LabelDefinitionService struct {
	repository LabelDefinitionApplicationRepository
	now        func() time.Time
}

func NewLabelDefinitionService(repository LabelDefinitionApplicationRepository) *LabelDefinitionService {
	return &LabelDefinitionService{repository: repository, now: time.Now}
}

func (service *LabelDefinitionService) Prepare(
	ctx context.Context, proposed LabelDefinition, subject, rationale string, tombstone bool,
) (LabelDefinition, string, error) {
	proposed.ID = strings.TrimSpace(proposed.ID)
	proposed.Description = strings.TrimSpace(proposed.Description)
	proposed.SuggestedResourceTypes = normalizedStrings(proposed.SuggestedResourceTypes)
	proposed.AcceptedValues = normalizedStrings(proposed.AcceptedValues)
	proposed.Rationale, proposed.UpdatedBy = strings.TrimSpace(rationale), strings.TrimSpace(subject)
	proposed.ChangeRequestID = ""
	proposed.UpdatedAt = service.now().UTC()
	existing, err := service.repository.GetLabelDefinition(ctx, proposed.ID)
	changeType := ChangeTypeCreateLabelDefinition
	switch {
	case errors.Is(err, ErrNotFound):
		if tombstone {
			return LabelDefinition{}, "", ErrNotFound
		}
		proposed.Status, proposed.CreatedBy, proposed.CreatedAt, proposed.Revision = LabelDefinitionActive, proposed.UpdatedBy, proposed.UpdatedAt, 1
	case err != nil:
		return LabelDefinition{}, "", err
	default:
		if existing.Status == LabelDefinitionTombstoned {
			return LabelDefinition{}, "", ErrInvalidTransition
		}
		proposed.CreatedBy, proposed.CreatedAt, proposed.Revision = existing.CreatedBy, existing.CreatedAt, existing.Revision+1
		if tombstone {
			proposed = existing
			proposed.Status, proposed.Rationale, proposed.UpdatedBy, proposed.UpdatedAt, proposed.Revision =
				LabelDefinitionTombstoned, strings.TrimSpace(rationale), strings.TrimSpace(subject), service.now().UTC(), existing.Revision+1
			changeType = ChangeTypeTombstoneLabelDefinition
		} else {
			proposed.Status = LabelDefinitionActive
			changeType = ChangeTypeUpdateLabelDefinition
		}
	}
	if err := validateLabelDefinition(proposed, false); err != nil {
		return LabelDefinition{}, "", err
	}
	return proposed, changeType, nil
}

func (service *LabelDefinitionService) ApplyApprovedChangeRequest(
	ctx context.Context, request ChangeRequest,
) (LabelDefinition, ChangeRequest, error) {
	if request.TargetKind != LabelDefinitionChangeTarget || request.Status != ChangeApproved || len(request.Changes) != 1 ||
		request.Changes[0].Request["definition"] == nil {
		return LabelDefinition{}, request, ErrInvalidTransition
	}
	change := request.Changes[0]
	if change.Type != ChangeTypeCreateLabelDefinition && change.Type != ChangeTypeUpdateLabelDefinition &&
		change.Type != ChangeTypeTombstoneLabelDefinition {
		return LabelDefinition{}, request, ErrInvalidTransition
	}
	encoded, marshalErr := json.Marshal(change.Request["definition"])
	if marshalErr != nil {
		return LabelDefinition{}, request, ErrConflict
	}
	var definition LabelDefinition
	labelID, _ := change.Request["labelId"].(string)
	if json.Unmarshal(encoded, &definition) != nil || labelID != definition.ID ||
		request.TargetID != labelDefinitionProposalID(definition) {
		return LabelDefinition{}, request, ErrConflict
	}
	if request.Execution != nil && request.Execution.Status == ExecutionSucceeded {
		stored, err := service.repository.GetLabelDefinition(ctx, definition.ID)
		return stored, request, err
	}
	definition.ChangeRequestID = request.ID
	stored, err := service.repository.PutLabelDefinition(ctx, definition)
	if errors.Is(err, ErrConflict) {
		if existing, getErr := service.repository.GetLabelDefinition(ctx, definition.ID); getErr == nil &&
			existing.Revision == definition.Revision && labelDefinitionsEqual(existing, definition) {
			stored, err = existing, nil
		}
	}
	now := service.now().UTC()
	if err != nil {
		failed, updateErr := service.repository.UpdateChangeRequest(ctx, request.ID, func(record *ChangeRequest) error {
			if record.Execution != nil && record.Execution.Status == ExecutionSucceeded {
				return ErrConflict
			}
			record.Execution = &ChangeExecution{Status: ExecutionFailed, StartedAt: now, CompletedAt: now, Items: []ChangeExecutionItem{{
				ChangeID: change.ID, ResourceRID: "label/" + definition.ID, Status: ExecutionFailed, Error: errorReason(err),
			}}}
			record.UpdatedAt = now
			return nil
		})
		if updateErr == nil {
			request = failed
		}
		return LabelDefinition{}, request, err
	}
	updated, updateErr := service.repository.UpdateChangeRequest(ctx, request.ID, func(record *ChangeRequest) error {
		if record.Status != ChangeApproved {
			return ErrInvalidTransition
		}
		if record.Execution != nil && record.Execution.Status == ExecutionSucceeded {
			return nil
		}
		record.Execution = &ChangeExecution{Status: ExecutionSucceeded, StartedAt: now, CompletedAt: now, Items: []ChangeExecutionItem{{
			ChangeID: change.ID, ResourceRID: "label/" + definition.ID, Status: ExecutionSucceeded,
			ResultID: fmt.Sprintf("label-definition/%s/%d", definition.ID, definition.Revision),
		}}}
		record.UpdatedAt = now
		return nil
	})
	if updateErr == nil {
		request = updated
	}
	return stored, request, updateErr
}

func labelDefinitionsEqual(left, right LabelDefinition) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func labelDefinitionProposalID(definition LabelDefinition) string {
	identity := definition
	identity.CreatedAt, identity.UpdatedAt = time.Time{}, time.Time{}
	encoded, _ := json.Marshal(identity)
	return fmt.Sprintf("%s:%d:%s", definition.ID, definition.Revision, stableShortID(string(encoded)))
}

func validateLabelDefinition(definition LabelDefinition, persisted bool) error {
	invalid := func(field string) error { return fmt.Errorf("%w: invalid label definition %s", ErrConflict, field) }
	if !labelIDPattern.MatchString(definition.ID) {
		return invalid("id")
	}
	if strings.TrimSpace(definition.Description) == "" {
		return invalid("description")
	}
	if len(definition.SuggestedResourceTypes) == 0 {
		return invalid("suggested resource types")
	}
	if len(definition.AcceptedValues) == 0 {
		return invalid("accepted values")
	}
	if strings.TrimSpace(definition.Rationale) == "" {
		return invalid("rationale")
	}
	if strings.TrimSpace(definition.CreatedBy) == "" || strings.TrimSpace(definition.UpdatedBy) == "" {
		return invalid("actor")
	}
	if definition.CreatedAt.IsZero() || definition.UpdatedAt.IsZero() {
		return invalid("timestamp")
	}
	if definition.Revision == 0 {
		return invalid("revision")
	}
	if definition.Status != LabelDefinitionActive && definition.Status != LabelDefinitionTombstoned {
		return fmt.Errorf("%w: invalid label definition status", ErrConflict)
	}
	for _, resourceType := range definition.SuggestedResourceTypes {
		if !labelIDPattern.MatchString(resourceType) {
			return fmt.Errorf("%w: invalid suggested resource type", ErrConflict)
		}
	}
	for _, value := range definition.AcceptedValues {
		if strings.TrimSpace(value) == "" || len(value) > 128 {
			return fmt.Errorf("%w: invalid accepted label value", ErrConflict)
		}
	}
	if persisted && strings.TrimSpace(definition.ChangeRequestID) == "" {
		return fmt.Errorf("%w: label definition requires Change Request evidence", ErrConflict)
	}
	return nil
}

type labelReference struct {
	ResourceType string
	Value        string
}

func validateDefinitionReferences(definition LabelDefinition, references []labelReference) error {
	if definition.Status == LabelDefinitionTombstoned && len(references) > 0 {
		return fmt.Errorf("%w: label %s is still referenced", ErrConflict, definition.ID)
	}
	accepted := stringSet(definition.AcceptedValues)
	for _, reference := range references {
		if reference.Value != "" {
			if _, ok := accepted[reference.Value]; !ok {
				return fmt.Errorf("%w: label %s value %q is still referenced but no longer accepted", ErrConflict, definition.ID, reference.Value)
			}
		}
	}
	return nil
}

func labelReferences(id string, plans []PlanRecord, teams []Team, pipelines []ExportPipeline) []labelReference {
	result := []labelReference{}
	for _, record := range latestPlansByEnvironment(plans) {
		if record.DesiredState == nil {
			continue
		}
		if value, ok := record.DesiredState.Environment.Labels[id]; ok {
			result = append(result, labelReference{"environment", value})
		}
		for _, entity := range record.DesiredState.Entities {
			if value, ok := entity.Labels[id]; ok {
				result = append(result, labelReference{"entity", value})
			}
		}
		for _, release := range record.DesiredState.Releases {
			if value, ok := release.Labels[id]; ok {
				result = append(result, labelReference{"product-release", value})
			}
		}
	}
	for _, team := range teams {
		if value, ok := team.Labels[id]; ok {
			result = append(result, labelReference{"team", value})
		}
	}
	for _, pipeline := range pipelines {
		if pipeline.Status == ExportPipelineDeleted {
			continue
		}
		for _, requirement := range append(append([]BundleLabelRequirement(nil), pipeline.RequiredLabels...), pipeline.ExcludedLabels...) {
			if requirement.Name == id {
				if len(requirement.Values) == 0 {
					result = append(result, labelReference{"export-pipeline", ""})
				}
				for _, value := range requirement.Values {
					result = append(result, labelReference{"export-pipeline", value})
				}
			}
		}
	}
	return result
}

func validateLabelAssignment(ctx context.Context, repository LabelDefinitionRepository, id, value string) error {
	definition, err := repository.GetLabelDefinition(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil // legacy labels remain valid until a definition is introduced
	}
	if err != nil {
		return err
	}
	if definition.Status != LabelDefinitionActive || !contains(definition.AcceptedValues, value) {
		return fmt.Errorf("%w: label %s does not accept value %q", ErrConflict, id, value)
	}
	return nil
}

func validateDesiredStateLabelDefinitions(ctx context.Context, repository LabelDefinitionRepository, state DesiredState) error {
	assignments := []map[string]string{state.Environment.Labels}
	for _, entity := range state.Entities {
		assignments = append(assignments, entity.Labels)
	}
	for _, release := range state.Releases {
		assignments = append(assignments, release.Labels)
	}
	for _, labels := range assignments {
		for id, value := range labels {
			if err := validateLabelAssignment(ctx, repository, id, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateLabelsFromDefinitions(labels map[string]string, definitions map[string]LabelDefinition) error {
	for id, value := range labels {
		definition, defined := definitions[id]
		if !defined {
			continue
		}
		if definition.Status != LabelDefinitionActive || !contains(definition.AcceptedValues, value) {
			return fmt.Errorf("%w: label %s does not accept value %q", ErrConflict, id, value)
		}
	}
	return nil
}

func validateDesiredStateLabelsFromDefinitions(state DesiredState, definitions map[string]LabelDefinition) error {
	if err := validateLabelsFromDefinitions(state.Environment.Labels, definitions); err != nil {
		return err
	}
	for _, entity := range state.Entities {
		if err := validateLabelsFromDefinitions(entity.Labels, definitions); err != nil {
			return err
		}
	}
	for _, release := range state.Releases {
		if err := validateLabelsFromDefinitions(release.Labels, definitions); err != nil {
			return err
		}
	}
	return nil
}

func validatePipelineLabelsFromDefinitions(pipeline ExportPipeline, definitions map[string]LabelDefinition) error {
	for _, requirement := range append(append([]BundleLabelRequirement(nil), pipeline.RequiredLabels...), pipeline.ExcludedLabels...) {
		definition, defined := definitions[requirement.Name]
		if !defined {
			continue
		}
		if definition.Status != LabelDefinitionActive {
			return fmt.Errorf("%w: label %s is tombstoned", ErrConflict, requirement.Name)
		}
		for _, value := range requirement.Values {
			if !contains(definition.AcceptedValues, value) {
				return fmt.Errorf("%w: label %s does not accept value %q", ErrConflict, requirement.Name, value)
			}
		}
	}
	return nil
}

func labelDefinitionIndex(definitions []LabelDefinition) map[string]LabelDefinition {
	result := make(map[string]LabelDefinition, len(definitions))
	for _, definition := range definitions {
		result[definition.ID] = definition
	}
	return result
}

func sortLabelDefinitions(definitions []LabelDefinition) {
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].ID != definitions[j].ID {
			return definitions[i].ID < definitions[j].ID
		}
		return definitions[i].Revision < definitions[j].Revision
	})
}
