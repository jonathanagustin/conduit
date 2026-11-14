// Package application records hand-off of an existing signed bundle and the
// install report a site sends after it checks in. It does not build a second
// archive.
package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// Handoff is one offline bundle handed to a site.
type Handoff struct {
	ID       string    `json:"id"`
	BundleID string    `json:"bundleId"`
	Site     string    `json:"site"`
	Digest   string    `json:"digest"`
	TakenBy  string    `json:"takenBy"`
	At       time.Time `json:"at"`
	Status   string    `json:"status"`
	Report   string    `json:"report,omitempty"`
}

// Verifier checks an existing bundle and returns its digest.
type Verifier interface {
	Verify(ctx context.Context, bundleID string) (string, error)
}

// Presence reports whether a site has checked in online.
type Presence interface {
	Online(ctx context.Context, site string) (bool, error)
}

// Service stores hand-offs.
type Service struct {
	Store    controlplane.Repository
	Verifier Verifier
	Presence Presence
	Now      func() time.Time
	mu       sync.Mutex
	handoffs map[string]Handoff
	pending  map[string]string
}

func (service *Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

// HandOff records who took a verified offline bundle. Confirmation is required.
func (service *Service) HandOff(ctx context.Context, actor, bundleID, site string, confirmed bool) (Handoff, string, error) {
	if !confirmed {
		return Handoff{}, "confirmation is required", nil
	}
	digest, err := service.Verifier.Verify(ctx, bundleID)
	if err != nil {
		return Handoff{}, "", err
	}
	if strings.TrimSpace(digest) == "" || strings.TrimSpace(site) == "" || strings.TrimSpace(actor) == "" {
		return Handoff{}, "", fmt.Errorf("offline bundle hand-off needs a verified digest, a site, and a person")
	}
	handoff := Handoff{
		ID: "obh_" + bundleID + "_" + site, BundleID: bundleID, Site: site, Digest: digest,
		TakenBy: actor, At: service.now().UTC(), Status: "handed_off",
	}
	service.mu.Lock()
	if service.handoffs == nil {
		service.handoffs = map[string]Handoff{}
	}
	service.handoffs[handoff.ID] = handoff
	service.mu.Unlock()
	if err := service.audit(ctx, actor, handoff.ID, governance.DecisionAllow, "offline bundle handed off"); err != nil {
		return Handoff{}, "", err
	}
	return handoff, "offline bundle handed off", nil
}

// ProposeReport stores an install report. Before the site has checked in, the
// report stays pending. A successful install still needs a different approver.
func (service *Service) ProposeReport(ctx context.Context, actor controlplane.Principal, id, outcome string) (Handoff, string, error) {
	service.mu.Lock()
	handoff, ok := service.handoffs[id]
	service.mu.Unlock()
	if !ok {
		return Handoff{}, "offline bundle hand-off was not found", nil
	}
	online, err := service.Presence.Online(ctx, handoff.Site)
	if err != nil {
		return Handoff{}, "", err
	}
	if !online {
		handoff.Status = "pending_check_in"
		handoff.Report = outcome
		service.mu.Lock()
		service.handoffs[id] = handoff
		service.mu.Unlock()
		return handoff, "report-back is pending until the site checks in", nil
	}
	if outcome != "installed" {
		handoff.Status = outcome
		handoff.Report = outcome
		service.mu.Lock()
		service.handoffs[id] = handoff
		service.mu.Unlock()
		return handoff, "install report recorded", nil
	}
	request, err := controlplane.NewGovernanceService(service.Store).ProposeHumanChange(ctx, "OFFLINE_BUNDLE", id, handoff.Site, []controlplane.EntityChange{{
		ID: "install-report", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: id,
		Request: map[string]any{"digest": handoff.Digest, "outcome": outcome},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/" + handoff.Site, Name: handoff.Site},
			"installation": {RID: "installation/" + id, Name: id},
			"product":      {RID: "product/offline-bundle", Name: "offline-bundle"},
		},
	}}, actor, "Install report for offline bundle "+id, "site "+handoff.Site+" reported "+outcome)
	if err != nil {
		return Handoff{}, "", err
	}
	service.mu.Lock()
	if service.pending == nil {
		service.pending = map[string]string{}
	}
	service.pending[request.ID] = id
	service.mu.Unlock()
	return handoff, request.ID, nil
}

// ApproveInstall marks the hand-off installed when the approver is not the author.
func (service *Service) ApproveInstall(ctx context.Context, approver controlplane.Principal, changeID, rationale string) (Handoff, string, error) {
	service.mu.Lock()
	id := service.pending[changeID]
	handoff := service.handoffs[id]
	service.mu.Unlock()
	if id == "" {
		return Handoff{}, "change request is not pending", nil
	}
	if handoff.TakenBy == approver.Username {
		return handoff, "the author cannot approve their own change request", nil
	}
	if _, _, err := controlplane.NewGovernanceService(service.Store).Approve(ctx, changeID, approver, rationale); err != nil {
		return Handoff{}, "", err
	}
	handoff.Status = "installed"
	handoff.Report = "installed"
	service.mu.Lock()
	service.handoffs[id] = handoff
	delete(service.pending, changeID)
	service.mu.Unlock()
	return handoff, "install report recorded", nil
}

func (service *Service) audit(ctx context.Context, actor, id, decision, reason string) error {
	event := governance.NewAuthorizationCheckEvent(service.now(), actor, "offline_bundle.handoff", "offline-bundle/"+id, decision, reason, nil)
	_, err := service.Store.AppendAuthorizationAudit(ctx, event)
	return err
}
