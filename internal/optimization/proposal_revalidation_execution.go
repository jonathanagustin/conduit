package optimization

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"
)

// ProposalEvidenceStore is the narrow durable boundary required to replay an
// authority-bearing proposal decision after process restart.
type ProposalEvidenceStore interface {
	TemporalFactSink
	GetFact(string) (TemporalFact, error)
	SaveProposal(AdvisoryPlanProposal) error
	GetProposal(string) (AdvisoryPlanProposal, error)
	SaveAcquisitionReceipt(AcquisitionExecutionReceipt) error
	GetAcquisitionReceipt(string) (AcquisitionExecutionReceipt, error)
	SaveRevalidation(ProposalRevalidation) error
	GetRevalidation(string) (ProposalRevalidation, error)
}

type FileProposalEvidenceStore struct{ Directory string }

func (store FileProposalEvidenceStore) twin() FileTwinStore {
	return FileTwinStore(store)
}
func (store FileProposalEvidenceStore) SaveFact(fact TemporalFact) error {
	return store.twin().SaveFact(fact)
}
func (store FileProposalEvidenceStore) GetFact(id string) (TemporalFact, error) {
	return store.twin().GetFact(id)
}

func (store FileProposalEvidenceStore) SaveProposal(value AdvisoryPlanProposal) error {
	if err := VerifyAdvisoryPlanProposal(value); err != nil {
		return err
	}
	return store.twin().save("optimizer-proposals", value.ID, value)
}
func (store FileProposalEvidenceStore) GetProposal(id string) (AdvisoryPlanProposal, error) {
	var value AdvisoryPlanProposal
	if !validContentID(id, "optprop_") {
		return value, errors.New("invalid optimizer proposal id")
	}
	if err := store.twin().get("optimizer-proposals", id, &value); err != nil {
		return value, err
	}
	return value, VerifyAdvisoryPlanProposal(value)
}
func (store FileProposalEvidenceStore) SaveAcquisitionReceipt(value AcquisitionExecutionReceipt) error {
	if err := VerifyAcquisitionExecutionReceipt(value); err != nil {
		return err
	}
	return store.twin().save("optimizer-acquisitions", value.ID, value)
}
func (store FileProposalEvidenceStore) GetAcquisitionReceipt(id string) (AcquisitionExecutionReceipt, error) {
	var value AcquisitionExecutionReceipt
	if !validContentID(id, "acq_") {
		return value, errors.New("invalid optimizer acquisition receipt id")
	}
	if err := store.twin().get("optimizer-acquisitions", id, &value); err != nil {
		return value, err
	}
	return value, VerifyAcquisitionExecutionReceipt(value)
}
func (store FileProposalEvidenceStore) SaveRevalidation(value ProposalRevalidation) error {
	if err := verifyStandaloneProposalRevalidation(value); err != nil {
		return err
	}
	return store.twin().save("optimizer-revalidations", value.ID, value)
}
func (store FileProposalEvidenceStore) GetRevalidation(id string) (ProposalRevalidation, error) {
	var value ProposalRevalidation
	if !validContentID(id, "optrev_") {
		return value, errors.New("invalid optimizer revalidation id")
	}
	if err := store.twin().get("optimizer-revalidations", id, &value); err != nil {
		return value, err
	}
	return value, verifyStandaloneProposalRevalidation(value)
}

func verifyStandaloneProposalRevalidation(value ProposalRevalidation) error {
	if value.APIVersion != AdvisoryProposalAPIVersion || value.Kind != "ProposalRevalidation" || !validContentID(value.ID, "optrev_") || !validContentID(value.ProposalID, "optprop_") || !validContentID(value.AcquisitionReceiptID, "acq_") {
		return errors.New("invalid authoritative proposal revalidation")
	}
	want, err := proposalRevalidationID(value)
	if err != nil {
		return err
	}
	if want != value.ID {
		return errors.New("proposal revalidation identity does not match content")
	}
	return nil
}

// ProposalRevalidator owns each fresh acquisition. Callers supply neither
// facts nor an authored revalidation record.
type ProposalRevalidator struct {
	Executor   AcquisitionExecutor
	Candidates []AcquisitionCall
	Evidence   ProposalEvidenceStore
}

func (revalidator ProposalRevalidator) Execute(ctx context.Context, proposal AdvisoryPlanProposal, stage string) (ProposalRevalidation, error) {
	if revalidator.Evidence == nil {
		return ProposalRevalidation{}, errors.New("proposal revalidation requires immutable evidence store")
	}
	if err := VerifyAdvisoryPlanProposal(proposal); err != nil {
		return ProposalRevalidation{}, err
	}
	requirements, err := proposalAcquisitionRequirements(proposal)
	if err != nil {
		return ProposalRevalidation{}, err
	}
	executor := revalidator.Executor
	executor.Facts = revalidator.Evidence
	receipt, err := executor.Execute(ctx, requirements, append([]AcquisitionCall(nil), revalidator.Candidates...))
	if err != nil {
		return ProposalRevalidation{}, err
	}
	if err := VerifyAcquisitionExecutionReceipt(receipt); err != nil {
		return ProposalRevalidation{}, err
	}
	if !receipt.PlannedComplete || !receipt.Complete || len(receipt.UnmetRequirements) != 0 || !reflect.DeepEqual(receipt.RequirementIDs, acquisitionRequirementIDs(requirements)) {
		return ProposalRevalidation{}, errors.New("proposal revalidation acquisition is incomplete")
	}
	if err := revalidator.Evidence.SaveAcquisitionReceipt(receipt); err != nil {
		return ProposalRevalidation{}, err
	}
	facts := make([]TemporalFact, 0, len(receipt.FactIDs))
	for _, id := range receipt.FactIDs {
		fact, err := revalidator.Evidence.GetFact(id)
		if err != nil {
			return ProposalRevalidation{}, fmt.Errorf("resolve acquired fact %s: %w", id, err)
		}
		facts = append(facts, fact)
	}
	finishedAt, _ := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	twinRequirements := proposalTwinRequirements(proposal)
	snapshot, err := BuildTwinSnapshot(twinRequirements, facts, finishedAt, finishedAt)
	if err != nil {
		return ProposalRevalidation{}, err
	}
	record, err := assessProposalRevalidation(proposal, stage, receipt.ID, snapshot)
	if err != nil {
		return ProposalRevalidation{}, err
	}
	if err := VerifyAuthoritativeProposalRevalidation(proposal, record, receipt); err != nil {
		return ProposalRevalidation{}, err
	}
	if err := revalidator.Evidence.SaveRevalidation(record); err != nil {
		return ProposalRevalidation{}, err
	}
	return record, nil
}

func VerifyAuthoritativeProposalRevalidation(proposal AdvisoryPlanProposal, record ProposalRevalidation, receipt AcquisitionExecutionReceipt) error {
	if err := VerifyProposalRevalidation(proposal, record); err != nil {
		return err
	}
	if err := VerifyAcquisitionExecutionReceipt(receipt); err != nil {
		return err
	}
	if record.AcquisitionReceiptID == "" || record.AcquisitionReceiptID != receipt.ID || !receipt.Complete || len(receipt.UnmetRequirements) != 0 {
		return errors.New("proposal revalidation is not bound to complete acquisition")
	}
	if !reflect.DeepEqual(receipt.RequirementIDs, acquisitionRequirementIDsMust(proposal)) {
		return errors.New("proposal revalidation acquisition covers foreign requirements")
	}
	factIDs := []string{}
	for _, observation := range record.Observations {
		factIDs = append(factIDs, observation.CurrentFactIDs...)
	}
	sort.Strings(factIDs)
	if !reflect.DeepEqual(factIDs, receipt.FactIDs) {
		return errors.New("proposal revalidation snapshot does not bind exact acquired facts")
	}
	return nil
}

func proposalAcquisitionRequirements(proposal AdvisoryPlanProposal) ([]StateRequirement, error) {
	requirements := make([]StateRequirement, 0, len(proposal.Guards))
	for _, guard := range proposal.Guards {
		if guard.Requirement.MaximumStalenessSeconds > int64(^uint(0)>>1) {
			return nil, errors.New("proposal guard freshness exceeds acquisition integer bound")
		}
		requirements = append(requirements, StateRequirement{ID: guard.Requirement.ID, Plane: guard.Requirement.Plane, MinimumFidelity: guard.Requirement.MinimumFidelity, MaximumStalenessSeconds: int(guard.Requirement.MaximumStalenessSeconds)})
	}
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	return requirements, nil
}

func acquisitionRequirementIDs(requirements []StateRequirement) []string {
	result := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		result = append(result, requirement.ID)
	}
	sort.Strings(result)
	return result
}
func acquisitionRequirementIDsMust(proposal AdvisoryPlanProposal) []string {
	requirements, _ := proposalAcquisitionRequirements(proposal)
	return acquisitionRequirementIDs(requirements)
}
