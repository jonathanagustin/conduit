// Package application opens a change request from one drifted fleet row.
// It uses the fleet projection the hub already builds and does not apply the fix.
package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// Result is the proposal. Allowed is false when the row is not drifted.
type Result struct {
	Allowed         bool
	Reason          string
	ChangeRequestID string
	Status          string
	AuditID         string
}

// Store is the repository the fleet and governance already share.
type Store interface {
	controlplane.Repository
}

// Governance records proposals and reviews through the Hub's configured service.
type Governance interface {
	ProposeHumanChange(context.Context, string, string, string, []controlplane.EntityChange, controlplane.Principal, string, string) (controlplane.ChangeRequest, error)
}

// Service proposes a fix.
type Service struct {
	Governance Governance
	Store      Store
	Now        func() time.Time
}

func (service Service) governance() Governance {
	if service.Governance != nil {
		return service.Governance
	}
	return controlplane.NewGovernanceService(service.Store)
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

// Propose opens one change request for a drifted installation. The caller
// cannot approve it. A row that is not drifted is refused and audited.
func (service Service) Propose(ctx context.Context, actor controlplane.Principal, environment, installation string) (Result, error) {
	environment = strings.TrimSpace(environment)
	installation = strings.TrimSpace(installation)
	if environment == "" || installation == "" || strings.TrimSpace(actor.Username) == "" {
		return Result{}, fmt.Errorf("environment, installation, and actor are required")
	}
	projection, err := controlplane.BuildFleetProjection(ctx, service.Store, controlplane.FleetQuery{Environment: environment}, service.now())
	if err != nil {
		return Result{}, err
	}
	var row controlplane.FleetInstallation
	found := false
	for _, candidate := range projection.Installations {
		if candidate.Environment == environment && candidate.InstallationID == installation {
			row = candidate
			found = true
			break
		}
	}
	resource := "environment/" + environment + "/installation/" + installation
	if !found || !drifted(row) {
		reason := "installation is not drifted"
		if !found {
			reason = "installation was not found"
		}
		auditID, err := service.audit(ctx, actor.Username, resource, governance.DecisionDeny, reason)
		if err != nil {
			return Result{}, err
		}
		return Result{Reason: reason, AuditID: auditID}, nil
	}
	reason := driftReason(row)
	governanceService := service.governance()
	request, err := governanceService.ProposeHumanChange(ctx, "INSTALLATION", installation, environment, []controlplane.EntityChange{{
		ID: "propose-fix", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: installation,
		Request: map[string]any{
			"desiredVersion": row.DesiredVersion, "observedVersion": row.ObservedVersion,
			"desiredReleaseDigest": row.DesiredReleaseDigest, "observedReleaseDigest": row.ObservedReleaseDigest,
		},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/" + environment, Name: environment},
			"installation": {RID: "installation/" + environment + "/" + installation, Name: installation},
			"product":      {RID: "product/" + row.Product, Name: row.Product},
		},
	}}, actor, "Propose fix for "+row.Product, reason)
	if err != nil {
		return Result{}, err
	}
	auditID, err := service.audit(ctx, actor.Username, resource, governance.DecisionAllow, reason)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Allowed: true, Reason: reason, ChangeRequestID: request.ID, Status: request.Status, AuditID: auditID,
	}, nil
}

func (service Service) audit(ctx context.Context, actor, resource, decision, reason string) (string, error) {
	event := governance.NewAuthorizationCheckEvent(service.now(), actor, "fleet.propose_fix", resource, decision, reason, nil)
	saved, err := service.Store.AppendAuthorizationAudit(ctx, event)
	if err != nil {
		return "", err
	}
	return saved.ID, nil
}

func drifted(row controlplane.FleetInstallation) bool {
	if row.ObservedAt.IsZero() && row.ObservedVersion == "" && row.ObservedReleaseDigest == "" && row.ObservedConfigDigest == "" {
		return false
	}
	if row.ConfigStatus == controlplane.ConfigDrifted {
		return true
	}
	if row.DesiredReleaseDigest != "" && row.ObservedReleaseDigest != "" && row.DesiredReleaseDigest != row.ObservedReleaseDigest {
		return true
	}
	if row.DesiredVersion != "" && row.ObservedVersion != "" && row.DesiredVersion != row.ObservedVersion {
		return true
	}
	return false
}

func driftReason(row controlplane.FleetInstallation) string {
	parts := []string{}
	if row.DesiredVersion != "" || row.ObservedVersion != "" {
		parts = append(parts, fmt.Sprintf("desired version %s, observed version %s", blank(row.DesiredVersion), blank(row.ObservedVersion)))
	}
	if row.DesiredReleaseDigest != "" || row.ObservedReleaseDigest != "" {
		parts = append(parts, fmt.Sprintf("desired digest %s, observed digest %s", blank(row.DesiredReleaseDigest), blank(row.ObservedReleaseDigest)))
	}
	if row.ConfigStatus == controlplane.ConfigDrifted {
		parts = append(parts, "config status DRIFTED")
	}
	if len(parts) == 0 {
		return "desired and observed state differ"
	}
	return strings.Join(parts, "; ")
}

func blank(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unreported"
	}
	return value
}
