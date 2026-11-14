package conduitcli

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/operator"
)

type environmentList struct {
	Environments []controlplane.EnvironmentCatalogRecord `json:"environments"`
}

func (list environmentList) MarshalCSV() ([][]string, error) {
	rows := [][]string{{"id", "generation", "agent", "entities", "observedAt"}}
	for _, record := range list.Environments {
		agent, entities, observed := "", "", ""
		if record.Reported != nil {
			agent = record.Reported.AgentID
			entities = strconv.Itoa(len(record.Reported.Entities))
			observed = formatTableTime(record.Reported.ObservedAt)
		}
		rows = append(rows, []string{record.Environment.ID, record.Environment.Generation, agent, entities, observed})
	}
	return rows, nil
}

type planList struct {
	operator.PlanListResult
}

func (list planList) MarshalCSV() ([][]string, error) {
	rows := [][]string{{"id", "environment", "status", "steps", "approvals", "requiredApprovals", "updatedAt"}}
	for _, plan := range list.Plans {
		rows = append(rows, []string{
			plan.ID, plan.Environment, string(plan.Status),
			strconv.Itoa(plan.StepCount), strconv.Itoa(plan.ApprovalCount), strconv.Itoa(plan.RequiredApprovals),
			formatTableTime(plan.UpdatedAt),
		})
	}
	return rows, nil
}

// planStatusView keeps the Hub's Plan document as the JSON and YAML body and
// adds a one-row table of the fields an operator scans first.
type planStatusView struct {
	controlplane.PlanRecord
}

func (view planStatusView) MarshalJSON() ([]byte, error) {
	return json.Marshal(view.PlanRecord)
}

func (view planStatusView) MarshalCSV() ([][]string, error) {
	return [][]string{
		{"id", "environment", "status", "approvals", "requiredApprovals", "revision"},
		{
			view.ID, view.Plan.Environment, string(view.Status),
			strconv.Itoa(len(view.Approvals)), strconv.Itoa(view.RequiredApprovals),
			strconv.FormatUint(view.Revision, 10),
		},
	}, nil
}

type packagingRunList struct {
	operator.PackagingRunList
}

func (list packagingRunList) MarshalCSV() ([][]string, error) {
	rows := [][]string{{"runId", "phase", "outcome", "planId", "targetPlanRef"}}
	for _, run := range list.Items {
		rows = append(rows, []string{run.RunID, run.Phase, run.Outcome, run.PlanID, run.TargetPlanRef})
	}
	return rows, nil
}

type bundleVerification struct {
	Verified       bool   `json:"verified"`
	BundleID       string `json:"bundleId"`
	ManifestDigest string `json:"manifestDigest"`
	SigningKeyID   string `json:"signingKeyId"`
}

func (result bundleVerification) MarshalCSV() ([][]string, error) {
	return [][]string{
		{"verified", "bundleId", "manifestDigest", "signingKeyId"},
		{"true", result.BundleID, result.ManifestDigest, result.SigningKeyID},
	}, nil
}

func formatTableTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func planView(record controlplane.PlanRecord, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return planStatusView{PlanRecord: record}, nil
}
