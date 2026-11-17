package composition

import (
	"context"
	"net/http"
	"time"

	adminapp "github.com/jonathanagustin/conduit/internal/adminstatus/application"
	admin "github.com/jonathanagustin/conduit/internal/adminstatus/domain"
	adminhttp "github.com/jonathanagustin/conduit/internal/adminstatus/httpapi"
	adminports "github.com/jonathanagustin/conduit/internal/adminstatus/ports"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governanceapp "github.com/jonathanagustin/conduit/internal/governance/application"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

type adminGrantSource struct{ repository controlplane.Repository }

func (source adminGrantSource) ResourceGrantsInScope(ctx context.Context, scope string) ([]governance.ResourceGrant, error) {
	return source.repository.ListResourceGrants(ctx, scope)
}

func mountAdminStatus(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options) http.Handler {
	service := newGovernanceService(repository, options)
	decision := governanceapp.ResourceDecision{Authorizer: governanceapp.NewAuthorizer(adminGrantSource{repository}), Policy: governanceapp.AdministratorPolicy{BootstrapAdministrators: options.BootstrapAdministrators, AllowLocalDeveloperFallback: options.AllowInsecureOperatorIdentityHeaders}}
	driver := controlplane.PlanOrchestratorNative
	if options.AfterPlanApproved != nil {
		driver = controlplane.PlanOrchestratorTemporal
	}
	probe := hostAIProbe()
	source := adminStatusSource{repository: repository, packaging: hub.PackagingStatus, storage: hub.AdminStorage, orchestrator: driver, now: time.Now, ai: probe.components}
	// The same view an administrator reads, once in the startup log.
	logAICapabilities(probe.components())
	mux := http.NewServeMux()
	adminhttp.Register(mux, adminhttp.Config{Reader: adminapp.Service{Source: source}, Authorize: func(request *http.Request) int {
		principal, ok := packagingPrincipal(request, service, options)
		if !ok {
			return http.StatusUnauthorized
		}
		if !decision.Administrator(request.Context(), principal) {
			return http.StatusForbidden
		}
		return 0
	}})
	mux.Handle("/", legacy)
	return mux
}

type adminStatusSource struct {
	repository   controlplane.Repository
	packaging    packaginghttp.Reader
	storage      map[string]adminports.CapacityReader
	orchestrator string
	now          func() time.Time
	// ai reports the model provider and the AI capabilities. Nil reports
	// none of them rather than a guess.
	ai func() []admin.Component
}

func (source adminStatusSource) Read(ctx context.Context) (admin.Summary, error) {
	if err := ctx.Err(); err != nil {
		return admin.Summary{}, err
	}
	now := source.now().UTC()
	summary := admin.Summary{GeneratedAt: now, Components: []admin.Component{{ID: "hub", Label: "Hub", Status: admin.Available, Detail: "This Hub served the status request"}, {ID: "orchestrator", Label: "Plan orchestrator", Status: "CONFIGURED", Detail: source.orchestrator + " driver configured; worker liveness is not measured"}}, Counters: []admin.Counter{}, Storage: []admin.Storage{}, Audit: admin.Audit{Status: admin.Unavailable, Detail: "This repository does not provide stored-chain verification"}}
	pending := admin.MissingCount("pendingChangeRequests", "Pending change requests", "Change-request store unavailable", "rm07-change-requests")
	if requests, err := source.repository.ListChangeRequests(ctx, ""); err == nil {
		var count int64
		for _, request := range requests {
			if request.Status == controlplane.ChangePendingApprovals {
				count++
			}
		}
		pending = admin.Count(pending.ID, pending.Label, count, "Stored requests awaiting approval", pending.Link)
	}
	summary.Counters = append(summary.Counters, pending)
	recalls := admin.MissingCount("openRecallDeclarations", "Open recall declarations", "Desired-state snapshots unavailable", "rm06-promotion-pipe")
	if plans, err := source.repository.ListPlans(ctx, ""); err == nil {
		var count int64
		for _, state := range controlplane.LatestDesiredSnapshots(plans) {
			for _, recall := range state.Recalls {
				if !recall.Reverted {
					count++
				}
			}
		}
		recalls = admin.Count(recalls.ID, recalls.Label, count, "Per-environment declarations in latest recorded desired snapshots; not withdrawal acknowledgments", recalls.Link)
	}
	summary.Counters = append(summary.Counters, recalls)
	live := admin.MissingCount("liveSpokes", "Live spokes", "Environment reports unavailable", "area-fleet")
	stale := admin.MissingCount("staleSpokes", "Stale spokes", "Environment reports unavailable", "area-fleet")
	unreachable := admin.MissingCount("unreachableSpokes", "Unreachable spokes", "Environment reports unavailable", "area-fleet")
	if environments, err := controlplane.ListEnvironmentCatalog(ctx, source.repository); err == nil {
		counts := map[string]int64{}
		for _, environment := range environments {
			counts[controlplane.ReportedSpokeStatus(environment.Reported, now).State]++
		}
		detail := "Per environment; live means a report within the existing 45-second freshness window"
		live = admin.Count(live.ID, live.Label, counts[controlplane.SpokeLive], detail, live.Link)
		stale = admin.Count(stale.ID, stale.Label, counts[controlplane.SpokeStale], detail, stale.Link)
		unreachable = admin.Count(unreachable.ID, unreachable.Label, counts[controlplane.SpokeUnreachable], detail, unreachable.Link)
	}
	summary.Counters = append(summary.Counters, live, stale, unreachable)
	runs := admin.MissingCount("activePackagingRuns", "Active packaging runs", "No packaging status reader configured", "area-packaging")
	worker := admin.Component{ID: "packaging", Label: "Packaging status reader", Status: "UNCONFIGURED", Detail: runs.Detail}
	if source.packaging != nil {
		worker.Status = admin.Unavailable
		worker.Detail = "Packaging status could not be read"
		if statuses, err := source.packaging.List(ctx); err == nil {
			var count int64
			known := true
			for _, status := range statuses {
				switch status.Phase {
				case packaging.PhaseAssess, packaging.PhasePropose, packaging.PhaseValidate, packaging.PhaseReview, packaging.PhaseHandoff:
					count++
				case packaging.PhaseComplete, packaging.PhaseBlocked:
				default:
					known = false
				}
			}
			worker.Status = admin.Available
			worker.Detail = "Status queries are available; this is not a worker heartbeat"
			if known {
				runs = admin.Count(runs.ID, runs.Label, count, "Assessment, proposal, validation, review or handoff phases", runs.Link)
			} else {
				runs.Detail = "The reader returned an unrecognized phase"
			}
		}
	}
	summary.Components = append(summary.Components, worker)
	if source.ai != nil {
		summary.Components = append(summary.Components, source.ai()...)
	}
	summary.Counters = append(summary.Counters, runs)
	events := admin.MissingCount("auditRecords", "Audit records", "Audit store unavailable", "au01-audit")
	if records, err := source.repository.ListAuthorizationAudit(ctx, ""); err == nil {
		events = admin.Count(events.ID, events.Label, int64(len(records)), "Stored records, including legacy imports", events.Link)
	}
	summary.Counters = append(summary.Counters, events)
	if verifier, ok := source.repository.(interface {
		VerifyAuthorizationAudit(context.Context) (governance.AuditCheckpoint, error)
	}); ok {
		summary.Audit = admin.Audit{Status: "FAILED", Detail: "Stored-chain check unavailable or failed"}
		if checkpoint, err := verifier.VerifyAuthorizationAudit(ctx); err == nil && checkpoint.Validate() == nil {
			checked := source.now().UTC()
			length := checkpoint.Sequence
			summary.Audit = admin.Audit{Status: "VERIFIED", ChainLength: &length, VerifiedAt: &checked, Detail: "Stored chain links checked in one read-only snapshot; no external checkpoint asserted"}
		}
	}
	for _, entry := range []struct{ id, label string }{{"data", "Data filesystem"}, {"objectStore", "Object-store filesystem"}, {"registry", "Registry filesystem"}} {
		measurement := admin.MissingStorage(entry.id, entry.label, "No filesystem telemetry provider configured")
		if reader := source.storage[entry.id]; reader != nil {
			if used, total, err := reader.Capacity(ctx); err == nil {
				measurement = admin.Capacity(entry.id, entry.label, used, total)
			} else {
				measurement.Detail = "Filesystem measurement unavailable"
			}
		}
		summary.Storage = append(summary.Storage, measurement)
	}
	return summary, nil
}
