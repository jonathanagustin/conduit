package controlplane

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	catalogports "github.com/jonathanagustin/conduit/internal/catalog/ports"
	execution "github.com/jonathanagustin/conduit/internal/execution/domain"
	httpidentity "github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	reporting "github.com/jonathanagustin/conduit/internal/reporting/domain"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/buildinfo"
)

// Handler builds a throwaway in-memory control plane that accepts Agent and
// operator identity from plain request headers. It exists for tests and must
// not be used to serve anything: it is the one constructor here that turns the
// certificate requirement off, and it does so silently.
//
// Callers wiring a real Hub want NewHandler, whose zero-value HandlerOptions
// leaves both insecure paths closed. cmd/platform-delivery is the worked
// example, and refuses to start when a secure identity mode is half-configured.
func Handler() http.Handler {
	return NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureAgentIdentityHeaders:    true,
		AllowInsecureOperatorIdentityHeaders: true,
	})
}

type OperatorIdentityResolver func(*http.Request) (IdentityClaims, error)

// EntityConfigurationAdapter is an inbound port for externally recovered
// installation configuration contracts. Implementations may translate only
// configuration and secret requirements; Product/Release coordinates,
// Environment inventory, authorization, approval, and execution remain native.
type EntityConfigurationAdapter func(
	mode string,
	payload json.RawMessage,
	requiredSecretKeys map[string][]string,
) (*ManagedInstallationConfiguration, *UniversalInstallationConfiguration, []SecretRequirement, error)

type HandlerOptions struct {
	ConsoleHandler                       http.Handler
	AllowInsecureAgentIdentityHeaders    bool
	AllowInsecureOperatorIdentityHeaders bool
	OperatorIdentityResolver             OperatorIdentityResolver
	BootstrapAdministrators              []string
	BundlePrivateKey                     ed25519.PrivateKey
	TrustedBundleKeys                    map[string]ed25519.PublicKey
	ArtifactScanner                      ArtifactScanner
	ExportSchedulerContext               context.Context
	ExportSchedulerErrorHandler          func(error)
	EntityConfigurationAdapters          map[string]EntityConfigurationAdapter
	// RecallOpenings is told which recalls an accepted DesiredState puts in
	// force, so that a person learns one opened. Nil is a deployment with no
	// notification store, which raises nothing rather than failing.
	RecallOpenings catalogports.RecallOpenings
}

func NewHandler(repository Repository, options ...HandlerOptions) http.Handler {
	configuration := HandlerOptions{}
	if len(options) > 0 {
		configuration = options[0]
	}
	lifecycle := NewLifecycle(repository)
	governance := NewGovernanceService(repository)
	modules := NewModuleService(repository)
	profiles := NewEnvironmentProfileService(repository)
	retirements := NewCatalogRetirementService(repository)
	labelDefinitions := NewLabelDefinitionService(repository)
	bundlePrivateKey := configuration.BundlePrivateKey
	if len(bundlePrivateKey) == 0 {
		var err error
		bundlePrivateKey, err = GenerateBundleSigningKey()
		if err != nil {
			panic("generate ephemeral Bundle signing key: " + err.Error())
		}
	}
	bundles, err := NewBundleService(repository, bundlePrivateKey, configuration.TrustedBundleKeys)
	if err != nil {
		panic("configure Bundle service: " + err.Error())
	}
	if configuration.ExportSchedulerContext != nil {
		go bundles.RunExportScheduler(configuration.ExportSchedulerContext, configuration.ExportSchedulerErrorHandler)
	}
	releaseCompatibilityAPI := newApolloPublicAPI(repository, governance, configuration)
	mux := http.NewServeMux()
	if configuration.ConsoleHandler != nil {
		mux.Handle("GET /console/", configuration.ConsoleHandler)
		mux.HandleFunc("GET /console", func(w http.ResponseWriter, request *http.Request) {
			http.Redirect(w, request, "/console/", http.StatusPermanentRedirect)
		})
	}
	releaseCompatibilityAPI.register(mux)
	// The Apollo-shaped GraphQL interface. It fails to mount rather than mounts
	// wrong: New refuses a schema whose Conduit-native fields nothing backs, and
	// a control plane serving an interface it cannot honour is worse than one
	// that will not start.
	if err := registerApolloGraphQL(mux, releaseCompatibilityAPI, repository, governance, configuration); err != nil {
		panic("configure the Apollo GraphQL interface: " + err.Error())
	}
	mux.HandleFunc("POST /v1/product-publications/helm-chart", releaseCompatibilityAPI.publishHelmBundle)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, request *http.Request) {
		if _, err := repository.ListPlans(request.Context(), ""); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, buildinfo.Current())
	})
	mux.HandleFunc("GET /v1/session", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		projection, err := projectSession(
			request.Context(), governance, principal, configuration, request.URL.Query()["resourceRid"],
		)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, projection)
	})
	mux.HandleFunc("GET /v1/overview", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, BuildOverviewProjection(
			request.Context(), repository, governance, principal, configuration, time.Now(),
		))
	})
	mux.HandleFunc("PUT /v1/environments/{environmentID}/profile", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration,
			environmentRID(environment), OperationOperate) {
			return
		}
		profile, ok := decodeJSON[EnvironmentProfile](w, request)
		if !ok {
			return
		}
		if profile.EnvironmentID != "" && profile.EnvironmentID != environment {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "environment id must match the path"})
			return
		}
		profile.EnvironmentID = environment
		prepared, err := profiles.Prepare(request.Context(), profile, principal.Username, profile.Rationale)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		var before any
		if existing, getErr := repository.GetEnvironmentProfile(request.Context(), environment); getErr == nil {
			before = existing
		} else if !errors.Is(getErr, ErrNotFound) {
			writeLifecycleError(w, getErr)
			return
		}
		proposalID := environment + ":" + strconv.FormatUint(prepared.Revision, 10) + ":" + environmentProfileProposalIdentity(prepared)
		change := EntityChange{
			ID:   "chg_" + stableShortID("ENVIRONMENT_PROFILE\x00"+proposalID),
			Type: ChangeTypeUpdateEnvironmentProperties,
			Request: map[string]any{
				"deploymentId": environment, "owner": prepared.OwnerTeam,
				"enableOperationalResponsibility": prepared.OperationalResponsibilityEnabled,
				"coOwner":                         prepared.CoOwnerTeams, "accreditation": prepared.Accreditation,
				"registerToNamespaces": prepared.RegisteredNamespaces, "supportOverride": prepared.SupportOverride,
				"displayName": prepared.DisplayName, "description": prepared.Description, "readme": prepared.README,
				"profile": prepared,
			},
			Diff: map[string]ChangeDiffEntry{"profile": {Before: before, After: prepared}},
			ReferencedResources: map[string]ReferencedResource{
				"environment": governance.referencedResource(request.Context(), environmentRID(environment), environment, principal),
			},
		}
		changeRequest, policyApproves, err := governance.ProposeChanges(
			request.Context(), EnvironmentProfileChangeTarget,
			proposalID, environment, []EntityChange{change}, principal,
			"Update Environment profile for "+environment, prepared.Rationale,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		result := map[string]any{"changeRequest": changeRequest}
		// The record may predate the policy in force; see ProposeChanges.
		if policyApproves && changeRequest.Status == ChangeApproved {
			stored, executedRequest, applyErr := profiles.ApplyApprovedChangeRequest(request.Context(), changeRequest)
			if applyErr != nil {
				writeLifecycleError(w, applyErr)
				return
			}
			result["profile"], result["changeRequest"] = stored, executedRequest
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/profile", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration,
			environmentRID(environment), OperationReadResource) {
			return
		}
		profile, err := repository.GetEnvironmentProfile(request.Context(), environment)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profile)
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/profile/revisions", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration,
			environmentRID(environment), OperationReadResource) {
			return
		}
		revisions, err := repository.ListEnvironmentProfileRevisions(request.Context(), environment)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revisions": revisions})
	})
	registerCatalogRetirement := func(pattern, scope string) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, request *http.Request) {
			principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
			product := request.PathValue("product")
			if !ok || !requireResourceOperation(
				w, request, governance, principal, configuration, productRID(product), OperationProposeChange,
			) {
				return
			}
			body, ok := decodeJSON[struct {
				Rationale string `json:"rationale"`
			}](w, request)
			if !ok {
				return
			}
			version := ""
			if scope == CatalogRetirementRelease {
				version = request.PathValue("version")
			}
			prepared, err := retirements.Prepare(request.Context(), scope, product, version, principal.Username, body.Rationale)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			productResource := governance.referencedResource(request.Context(), productRID(product), product, principal)
			resources := map[string]ReferencedResource{"product": productResource}
			changeType := ChangeTypeRetireProduct
			if scope == CatalogRetirementRelease {
				changeType = ChangeTypeRetireProductRelease
				releaseResource := productResource
				releaseResource.RID = catalogRetirementRID(prepared)
				releaseResource.Name = product + ":" + version
				resources["release"] = releaseResource
			}
			targetID := catalogRetirementRID(prepared)
			change := EntityChange{
				ID: "chg_" + stableShortID(changeType+"\x00"+targetID), Type: changeType,
				Request:             map[string]any{"product": product, "version": version, "rationale": prepared.Rationale, "retirement": prepared},
				Diff:                map[string]ChangeDiffEntry{"retired": {Before: false, After: true}},
				ReferencedResources: resources,
			}
			changeRequest, policyApproves, err := governance.ProposeChanges(
				request.Context(), CatalogRetirementChangeTarget, targetID, "catalog", []EntityChange{change}, principal,
				"Retire "+targetID, prepared.Rationale,
			)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			result := map[string]any{"changeRequest": changeRequest}
			// The record may predate the policy in force; see ProposeChanges.
			if policyApproves && changeRequest.Status == ChangeApproved {
				stored, executed, applyErr := retirements.ApplyApprovedChangeRequest(request.Context(), changeRequest)
				if applyErr != nil {
					writeLifecycleError(w, applyErr)
					return
				}
				result["retirement"], result["changeRequest"] = stored, executed
			}
			writeJSON(w, http.StatusCreated, result)
		})
	}
	registerCatalogRetirement("POST /v1/products/{product}/retire", CatalogRetirementProduct)
	registerCatalogRetirement("POST /v1/products/{product}/releases/{version}/retire", CatalogRetirementRelease)
	mux.HandleFunc("GET /v1/catalog-retirements", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		product := request.URL.Query().Get("product")
		if !ok {
			return
		}
		if product == "" {
			if !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
				return
			}
		} else if !requireResourceOperation(w, request, governance, principal, configuration, productRID(product), OperationReadResource) {
			return
		}
		stored, err := repository.ListCatalogRetirements(request.Context(), product)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"retirements": stored})
	})
	registerLabelDefinitionWrite := func(pattern string, tombstone bool) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, request *http.Request) {
			principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
			if !ok {
				return
			}
			id := request.PathValue("labelID")
			existing, getErr := repository.GetLabelDefinition(request.Context(), id)
			resource, operation := "label/"+id, OperationOperate
			if errors.Is(getErr, ErrNotFound) && !tombstone {
				resource, operation = "global/label", OperationProposeChange
			}
			if getErr != nil && !errors.Is(getErr, ErrNotFound) {
				writeLifecycleError(w, getErr)
				return
			}
			if !requireResourceOperation(w, request, governance, principal, configuration, resource, operation) {
				return
			}
			var proposed LabelDefinition
			var rationale string
			if tombstone {
				body, decoded := decodeJSON[struct {
					Rationale string `json:"rationale"`
				}](w, request)
				if !decoded {
					return
				}
				proposed.ID, rationale = id, body.Rationale
			} else {
				body, decoded := decodeJSON[LabelDefinition](w, request)
				if !decoded {
					return
				}
				if body.ID != "" && body.ID != id {
					writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "label id must match the path"})
					return
				}
				body.ID = id
				proposed, rationale = body, body.Rationale
			}
			prepared, changeType, err := labelDefinitions.Prepare(request.Context(), proposed, principal.Username, rationale, tombstone)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			var before any
			if getErr == nil {
				before = existing
			}
			targetID := labelDefinitionProposalID(prepared)
			change := EntityChange{
				ID: "chg_" + stableShortID(changeType+"\x00"+targetID), Type: changeType,
				Request:             map[string]any{"labelId": id, "definition": prepared},
				Diff:                map[string]ChangeDiffEntry{"definition": {Before: before, After: prepared}},
				ReferencedResources: map[string]ReferencedResource{"label": governance.referencedResource(request.Context(), "label/"+id, id, principal)},
			}
			changeRequest, policyApproves, err := governance.ProposeChanges(request.Context(), LabelDefinitionChangeTarget, targetID, "catalog", []EntityChange{change}, principal, "Govern label definition "+id, prepared.Rationale)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			result := map[string]any{"changeRequest": changeRequest}
			// The record may predate the policy in force; see ProposeChanges.
			if policyApproves && changeRequest.Status == ChangeApproved {
				stored, executed, applyErr := labelDefinitions.ApplyApprovedChangeRequest(request.Context(), changeRequest)
				if applyErr != nil {
					writeLifecycleError(w, applyErr)
					return
				}
				result["definition"], result["changeRequest"] = stored, executed
			}
			writeJSON(w, http.StatusCreated, result)
		})
	}
	registerLabelDefinitionWrite("PUT /v1/labels/{labelID}", false)
	registerLabelDefinitionWrite("DELETE /v1/labels/{labelID}", true)
	mux.HandleFunc("GET /v1/labels", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		if !requireResourceOperation(w, request, governance, principal, configuration, "global/label", OperationReadResource) {
			return
		}
		definitions, err := repository.ListLabelDefinitions(request.Context(), request.URL.Query().Get("includeTombstoned") == "true")
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"definitions": definitions})
	})
	mux.HandleFunc("GET /v1/labels/{labelID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		id := request.PathValue("labelID")
		if !requireResourceOperation(w, request, governance, principal, configuration, "label/"+id, OperationReadResource) {
			return
		}
		definition, err := repository.GetLabelDefinition(request.Context(), id)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, definition)
	})
	mux.HandleFunc("GET /v1/labels/{labelID}/revisions", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		id := request.PathValue("labelID")
		if !requireResourceOperation(w, request, governance, principal, configuration, "label/"+id, OperationReadResource) {
			return
		}
		revisions, err := repository.ListLabelDefinitionRevisions(request.Context(), id)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revisions": revisions})
	})
	mux.HandleFunc("POST /v1/validate", func(w http.ResponseWriter, request *http.Request) {
		state, ok := decodeJSON[DesiredState](w, request)
		if !ok {
			return
		}
		errs := Validate(state)
		status := http.StatusOK
		if len(errs) > 0 {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, map[string]any{"valid": len(errs) == 0, "errors": errs})
	})
	mux.HandleFunc("POST /v1/apollo/product-manifests/validate", func(w http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(w, request.Body, 1<<20)
		manifest, errs, err := DecodeApolloProductManifest(request.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": err.Error()})
			return
		}
		status := http.StatusOK
		if len(errs) > 0 {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, map[string]any{"valid": len(errs) == 0, "errors": errs, "manifest": manifest})
	})
	mux.HandleFunc("POST /v1/apollo/environment-settings/validate", func(w http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(w, request.Body, 1<<20)
		contents, err := io.ReadAll(request.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": err.Error()})
			return
		}
		settings, err := DecodeApolloEnvironmentSettings(string(contents))
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"valid": false, "error": err.Error()})
			return
		}
		normalized, err := EncodeApolloEnvironmentSettings(settings)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"valid": true, "settings": settings, "normalized": normalized})
	})
	mux.HandleFunc("POST /v1/artifact-scans", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		if configuration.ArtifactScanner == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "external artifact scanner is not configured"})
			return
		}
		release, ok := decodeJSON[Release](w, request)
		if !ok {
			return
		}
		result, err := configuration.ArtifactScanner.Scan(request.Context(), release)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /v1/artifact-registries", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		records, err := ListArtifactRegistryRecords(request.Context(), repository)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		visible := records[:0]
		for _, record := range records {
			if isGovernanceAdministrator(request.Context(), governance, principal, configuration) ||
				governance.Authorize(request.Context(), effectivePrincipal(principal), environmentRID(record.Environment), OperationReadResource) {
				visible = append(visible, record)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifactRegistries": visible})
	})
	mux.HandleFunc("GET /v1/environments", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		records, err := ListEnvironmentCatalog(request.Context(), repository)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		visible := make([]EnvironmentCatalogRecord, 0, len(records))
		administrator := isGovernanceAdministrator(request.Context(), governance, principal, configuration)
		for _, record := range records {
			if administrator ||
				governance.Authorize(request.Context(), effectivePrincipal(principal), environmentRID(record.Environment.ID), OperationReadResource) {
				visible = append(visible, record)
			}
		}
		view := request.URL.Query().Get("view")
		if view != "" && view != "full" && view != "summary" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "view must be full or summary"})
			return
		}
		defaultLimit, maximumLimit := fullCatalogPageLimit, fullCatalogPageLimit
		if view == "summary" {
			defaultLimit, maximumLimit = defaultPageLimit, maxPageLimit
		}
		cursor, limit, err := parsePageQuery(request.URL.Query(), defaultLimit, maximumLimit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		page, err := paginateEnvironmentCatalog(visible, request.URL.Query().Get("search"), cursor, limit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		if view == "summary" {
			writeJSON(w, http.StatusOK, summarizeEnvironmentCatalog(page))
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, environmentRID(environment), OperationReadResource) {
			return
		}
		records, err := ListEnvironmentCatalog(request.Context(), repository)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		for _, record := range records {
			if record.Environment.ID == environment {
				writeJSON(w, http.StatusOK, record)
				return
			}
		}
		writeLifecycleError(w, ErrNotFound)
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/entities", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, environmentRID(environment), OperationReadResource) {
			return
		}
		plans, err := repository.ListPlans(request.Context(), environment)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		state, exists := latestDesiredStates(plans)[environment]
		if !exists {
			writeLifecycleError(w, ErrNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entities": state.Entities})
	})
	mux.HandleFunc("GET /v1/products", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		records, err := ListProductCatalog(request.Context(), repository)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		visible := make([]ProductCatalogRecord, 0, len(records))
		administrator := isGovernanceAdministrator(request.Context(), governance, principal, configuration)
		for _, record := range records {
			if administrator ||
				governance.Authorize(request.Context(), effectivePrincipal(principal), productRID(record.Product), OperationReadResource) {
				visible = append(visible, record)
			}
		}
		view := request.URL.Query().Get("view")
		if view != "" && view != "full" && view != "summary" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "view must be full or summary"})
			return
		}
		defaultLimit, maximumLimit := fullCatalogPageLimit, fullCatalogPageLimit
		if view == "summary" {
			defaultLimit, maximumLimit = defaultPageLimit, maxPageLimit
		}
		cursor, limit, err := parsePageQuery(request.URL.Query(), defaultLimit, maximumLimit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		page, err := paginateProductCatalog(visible, request.URL.Query().Get("search"), cursor, limit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		if view == "summary" {
			writeJSON(w, http.StatusOK, summarizeProductCatalog(page))
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /v1/products/{product}/releases", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		product := request.PathValue("product")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, productRID(product), OperationReadResource) {
			return
		}
		records, err := ListProductCatalog(request.Context(), repository)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		for _, record := range records {
			if record.Product == product {
				writeJSON(w, http.StatusOK, map[string]any{"releases": record.Releases})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"releases": []Release{}})
	})
	mux.HandleFunc("GET /v1/products/{product}/releases/{version}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		product := request.PathValue("product")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, productRID(product), OperationReadResource) {
			return
		}
		records, err := ListProductCatalog(request.Context(), repository)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		for _, record := range records {
			if record.Product == product {
				for _, release := range record.Releases {
					if release.Version == request.PathValue("version") {
						writeJSON(w, http.StatusOK, release)
						return
					}
				}
			}
		}
		writeLifecycleError(w, ErrNotFound)
	})
	mux.HandleFunc("GET /v1/products/{product}/publications", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		product := request.PathValue("product")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, productRID(product), OperationReadResource) {
			return
		}
		publications, err := repository.ListProductPublications(request.Context(), product)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"publications": publications})
	})
	mux.HandleFunc("POST /v1/product-publications/terraform", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, "global/artifact", OperationCreateArtifact) {
			return
		}
		manifest, ok := decodeJSON[TerraformProductManifest](w, request)
		if !ok {
			return
		}
		if err := ValidateTerraformProductManifest(manifest); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		product := manifest.ProductGroup + ":" + manifest.ProductName
		catalog, err := ListProductCatalog(request.Context(), repository)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		exists := false
		for _, candidate := range catalog {
			exists = exists || candidate.Product == product
		}
		if !exists && !requireResourceOperation(w, request, governance, principal, configuration, "global/product", OperationCreateProduct) {
			return
		}
		if !requireResourceOperation(w, request, governance, principal, configuration, productRID(product), OperationCreateRelease) {
			return
		}
		if _, err := repository.GetCatalogRetirement(request.Context(), product, ""); err == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Product coordinate is retired"})
			return
		} else if !errors.Is(err, ErrNotFound) {
			writeLifecycleError(w, err)
			return
		}
		if _, err := repository.GetCatalogRetirement(request.Context(), product, manifest.ProductVersion); err == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Product Release coordinate is retired"})
			return
		} else if !errors.Is(err, ErrNotFound) {
			writeLifecycleError(w, err)
			return
		}
		encoded, _ := json.Marshal(manifest)
		digest := sha256.Sum256(encoded)
		publication := ProductPublication{
			MavenCoordinate: product + ":" + manifest.ProductVersion, Product: product, Version: manifest.ProductVersion,
			Kind: "terraform", PublicationDigest: "sha256:" + hex.EncodeToString(digest[:]),
			ReleaseChannels: defaultChannelsForVersion(manifest.ProductVersion), CreatedAt: time.Now().UTC(),
		}
		stored, err := repository.CreateProductPublication(request.Context(), publication)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, stored)
	})
	mux.HandleFunc("GET /v1/modules", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		releases, err := repository.ListModuleReleases(request.Context(), "")
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
	})
	mux.HandleFunc("POST /v1/artifacts/blobs", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		uploadArtifactBlob(w, request, repository, "")
	})
	mux.HandleFunc("GET /v1/bundles/signing-key", func(w http.ResponseWriter, _ *http.Request) {
		id, key := bundles.PublicSigningKey()
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "algorithm": "Ed25519", "publicKey": key})
	})
	mux.HandleFunc("PUT /v1/export-pipelines/{pipelineID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		pipeline, ok := decodeJSON[ExportPipeline](w, request)
		if !ok {
			return
		}
		if pipeline.ID != "" && pipeline.ID != request.PathValue("pipelineID") {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "pipeline id must match the path"})
			return
		}
		pipeline.ID = request.PathValue("pipelineID")
		stored, err := bundles.PutPipeline(request.Context(), pipeline)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, stored)
	})
	mux.HandleFunc("GET /v1/export-pipelines", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		pipelines, err := repository.ListExportPipelines(request.Context())
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pipelines": pipelines})
	})
	mux.HandleFunc("POST /v1/export-pipelines/{pipelineID}/preview", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		preview, err := bundles.PreviewExportPipeline(request.Context(), request.PathValue("pipelineID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
	})
	mux.HandleFunc("GET /v1/export-pipelines/{pipelineID}/schedule-runs", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		runs, err := repository.ListExportPipelineScheduleRuns(request.Context(), request.PathValue("pipelineID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
	})
	registerPipelineLifecycle := func(pattern, changeType, title string, apply func(context.Context, string) (ExportPipeline, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, request *http.Request) {
			principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
			if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
				return
			}
			if !requireEmptyBody(w, request) {
				return
			}
			pipelineID := request.PathValue("pipelineID")
			change := EntityChange{
				ID: "chg_" + stableShortID(changeType+"\x00"+pipelineID), Type: changeType,
				Request: map[string]any{"pipelineRid": pipelineID}, Diff: map[string]ChangeDiffEntry{},
				ReferencedResources: map[string]ReferencedResource{"bundlingpipeline": {RID: "export-pipeline/" + pipelineID, Name: pipelineID}},
			}
			changeRequest, policyApproves, err := governance.ProposeChanges(request.Context(), "EXPORT_PIPELINE_LIFECYCLE_"+changeType, pipelineID,
				"platform", []EntityChange{change}, principal, title+" "+pipelineID, "")
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			result := map[string]any{"changeRequest": changeRequest}
			// The record may predate the policy in force; see ProposeChanges.
			if policyApproves && changeRequest.Status == ChangeApproved {
				pipeline, applyErr := apply(request.Context(), pipelineID)
				if applyErr != nil {
					writeLifecycleError(w, applyErr)
					return
				}
				result["pipeline"] = pipeline
			}
			writeJSON(w, http.StatusCreated, result)
		})
	}
	registerPipelineLifecycle("POST /v1/export-pipelines/{pipelineID}/archive", ChangeTypeArchivePipeline, "Archive Export Pipeline",
		func(ctx context.Context, id string) (ExportPipeline, error) {
			return bundles.SetPipelineArchived(ctx, id, true)
		})
	registerPipelineLifecycle("POST /v1/export-pipelines/{pipelineID}/unarchive", ChangeTypeUnarchivePipeline, "Unarchive Export Pipeline",
		func(ctx context.Context, id string) (ExportPipeline, error) {
			return bundles.SetPipelineArchived(ctx, id, false)
		})
	registerPipelineLifecycle("DELETE /v1/export-pipelines/{pipelineID}", ChangeTypeDeletePipeline, "Delete Export Pipeline", bundles.DeletePipeline)
	mux.HandleFunc("GET /v1/environments/{environmentID}/target-hub-versions", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration,
			environmentRID(environment), OperationReadResource) {
			return
		}
		versions, err := bundles.EnvironmentEntityVersions(request.Context(), environment)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, versions)
	})
	mux.HandleFunc("PUT /v1/export-pipelines/{pipelineID}/target-hub-versions/{targetHub}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, 1<<20)
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
			return
		}
		versions, err := DecodeTargetHubVersions(payload)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		pipeline, err := bundles.RecordTargetHubVersions(
			request.Context(), request.PathValue("pipelineID"), request.PathValue("targetHub"), versions,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pipeline)
	})
	mux.HandleFunc("POST /v1/export-pipelines/{pipelineID}/build", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		revision := uint64(0)
		if value := strings.TrimSpace(request.URL.Query().Get("revision")); value != "" {
			parsed, parseErr := strconv.ParseUint(value, 10, 64)
			if parseErr != nil || parsed == 0 {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "revision must be a positive integer"})
				return
			}
			revision = parsed
		}
		standaloneValue := request.URL.Query().Get("standalone")
		if standaloneValue == "" {
			standaloneValue = "false"
		}
		standalone, parseErr := strconv.ParseBool(standaloneValue)
		if parseErr != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "standalone must be true or false"})
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		built, err := bundles.BuildRevisionForTarget(request.Context(), request.PathValue("pipelineID"), revision,
			strings.TrimSpace(request.URL.Query().Get("targetHub")), standalone)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"bundles": built})
	})
	mux.HandleFunc("POST /v1/bundles/{bundleID}/transfer-confirmations", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		body, ok := decodeJSON[struct {
			TargetHub string `json:"targetHub"`
		}](w, request)
		if !ok {
			return
		}
		confirmation, err := bundles.ConfirmTransfer(request.Context(), request.PathValue("bundleID"), body.TargetHub)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, confirmation)
	})
	mux.HandleFunc("POST /v1/bundle-transfer-confirmations/{confirmationID}/rescind", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		confirmation, err := bundles.RescindTransferByID(request.Context(), request.PathValue("confirmationID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, confirmation)
	})
	mux.HandleFunc("GET /v1/bundles/{bundleID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		bundle, err := repository.GetBundle(request.Context(), request.PathValue("bundleID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, bundle)
	})
	mux.HandleFunc("POST /v1/bundles/preview", func(w http.ResponseWriter, request *http.Request) {
		envelope, ok := decodeJSON[BundleEnvelope](w, request)
		if !ok {
			return
		}
		preview, err := bundles.Preview(request.Context(), envelope)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
	})
	mux.HandleFunc("POST /v1/bundles/import", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Bundle              BundleEnvelope `json:"bundle"`
			RequireSecurityPass bool           `json:"requireSecurityPass,omitempty"`
			TargetEnvironments  []string       `json:"targetEnvironments,omitempty"`
		}](w, request)
		if !ok {
			return
		}
		environments := normalizedStrings(body.TargetEnvironments)
		if len(environments) == 0 {
			for _, resource := range body.Bundle.Manifest.Resources {
				if resource.Type == "ENVIRONMENT" {
					environments = append(environments, resource.ID)
				}
			}
			environments = normalizedStrings(environments)
		}
		if len(environments) == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "at least one target Environment is required"})
			return
		}
		imports := make([]BundleImport, 0, len(environments))
		changeRequests := make([]ChangeRequest, 0, len(environments))
		for _, environment := range environments {
			if !requireResourceOperation(w, request, governance, principal, configuration, environmentRID(environment), OperationImportBundle) {
				return
			}
			record, err := bundles.StageImport(request.Context(), body.Bundle, environment, principal.Username, body.RequireSecurityPass)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			changes, err := governance.bundleImportChanges(request.Context(), body.Bundle, environment, principal)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			changeRequest, policyApproves, err := governance.ProposeChanges(request.Context(), "BUNDLE_IMPORT", record.ID, environment, changes, principal, "Import Bundle "+body.Bundle.Manifest.ID+" into "+environment, "")
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			record, err = bundles.LinkImportChangeRequest(request.Context(), record.ID, changeRequest.ID)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			// The record may predate the policy in force; see ProposeChanges.
			if policyApproves && changeRequest.Status == ChangeApproved {
				record, err = bundles.AdmitImport(request.Context(), record.ID)
				if err != nil {
					writeLifecycleError(w, err)
					return
				}
			}
			imports, changeRequests = append(imports, record), append(changeRequests, changeRequest)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"imports": imports, "changeRequests": changeRequests})
	})
	mux.HandleFunc("GET /v1/bundle-imports", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		imports, err := repository.ListBundleImports(request.Context())
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"imports": imports})
	})
	mux.HandleFunc("PUT /v1/artifacts/blobs/{digest}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		uploadArtifactBlob(w, request, repository, request.PathValue("digest"))
	})
	mux.HandleFunc("GET /v1/artifacts/blobs/{digest}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		serveArtifactBlob(w, request, repository, true)
	})
	mux.HandleFunc("HEAD /v1/artifacts/blobs/{digest}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		serveArtifactBlob(w, request, repository, false)
	})
	mux.HandleFunc("POST /v1/modules/releases", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		body, ok := decodeJSON[struct {
			Definitions []ModuleDefinition `json:"definitions"`
		}](w, request)
		if !ok {
			return
		}
		releases, err := modules.PublishBatch(request.Context(), body.Definitions)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"releases": releases})
	})
	mux.HandleFunc("POST /v1/modules/lock", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		definition, ok := decodeJSON[ModuleDefinition](w, request)
		if !ok {
			return
		}
		lock, err := modules.Lock(request.Context(), definition)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, lock)
	})
	mux.HandleFunc("GET /v1/modules/{moduleName}/releases", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		releases, err := repository.ListModuleReleases(request.Context(), request.PathValue("moduleName"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
	})
	mux.HandleFunc("POST /v1/modules/{moduleName}/releases/{version}/promote", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		body, ok := decodeJSON[struct {
			Channels []string `json:"channels"`
		}](w, request)
		if !ok {
			return
		}
		release, err := modules.PromoteRelease(request.Context(), request.PathValue("moduleName"), request.PathValue("version"), body.Channels)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, release)
	})
	mux.HandleFunc("POST /v1/modules/{moduleName}/releases/{version}/recall", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		release, err := modules.RecallRelease(request.Context(), request.PathValue("moduleName"), request.PathValue("version"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, release)
	})
	mux.HandleFunc("POST /v1/environments/{environmentID}/module-installations", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, environmentRID(request.PathValue("environmentID")), OperationOperate) {
			return
		}
		body, ok := decodeJSON[struct {
			Module         string            `json:"module"`
			Version        string            `json:"version,omitempty"`
			ReleaseChannel string            `json:"releaseChannel,omitempty"`
			Variables      map[string]string `json:"variables,omitempty"`
		}](w, request)
		if !ok {
			return
		}
		installation, err := modules.InstallTrackingChannel(
			request.Context(), request.PathValue("environmentID"), body.Module, body.Version, body.ReleaseChannel, body.Variables,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, installation)
	})
	mux.HandleFunc("PUT /v1/modules/{module}/auto-update-policy", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, moduleRID(request.PathValue("module")), OperationOperate) {
			return
		}
		body, ok := decodeJSON[struct {
			Enabled   bool   `json:"enabled"`
			Rationale string `json:"rationale"`
			Revision  uint64 `json:"revision"`
		}](w, request)
		if !ok {
			return
		}
		policy, err := modules.ConfigureAutoUpdatePolicy(
			request.Context(), request.PathValue("module"), body.Enabled, body.Rationale, principal.Username, body.Revision,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	})
	mux.HandleFunc("GET /v1/modules/{module}/auto-update-policy", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, moduleRID(request.PathValue("module")), OperationReadResource) {
			return
		}
		policy, err := repository.GetModuleAutoUpdatePolicy(request.Context(), request.PathValue("module"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	})
	mux.HandleFunc("POST /v1/module-installations/{installationID}/pause", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireModuleInstallationOperation(w, request, repository, governance, principal, configuration, OperationOperate) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		installation, err := modules.PauseUpdates(request.Context(), request.PathValue("installationID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, installation)
	})
	mux.HandleFunc("POST /v1/module-installations/{installationID}/resume", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireModuleInstallationOperation(w, request, repository, governance, principal, configuration, OperationOperate) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		installation, err := modules.ResumeUpdates(request.Context(), request.PathValue("installationID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, installation)
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/module-installations", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, environmentRID(request.PathValue("environmentID")), OperationReadResource) {
			return
		}
		installations, err := repository.ListModuleInstallations(request.Context(), request.PathValue("environmentID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"installations": installations})
	})
	mux.HandleFunc("GET /v1/module-installations/{installationID}/update", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireModuleInstallationOperation(w, request, repository, governance, principal, configuration, OperationReadResource) {
			return
		}
		proposal, err := modules.ProposeUpdate(request.Context(), request.PathValue("installationID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, proposal)
	})
	mux.HandleFunc("POST /v1/module-installations/{installationID}/update", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireModuleInstallationOperation(w, request, repository, governance, principal, configuration, OperationOperate) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		installation, err := modules.ApplyUpdate(request.Context(), request.PathValue("installationID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, installation)
	})
	mux.HandleFunc("POST /v1/module-installations/{installationID}/update/automatic", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireModuleInstallationOperation(w, request, repository, governance, principal, configuration, OperationOperate) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		installation, changeRequest, err := modules.ApplyAutomaticUpdate(request.Context(), request.PathValue("installationID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"installation": installation, "changeRequest": changeRequest})
	})
	mux.HandleFunc("POST /v1/module-installations/{installationID}/unlink", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireModuleInstallationOperation(w, request, repository, governance, principal, configuration, OperationOperate) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		installation, err := modules.Unlink(request.Context(), request.PathValue("installationID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, installation)
	})
	mux.HandleFunc("POST /v1/plans", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		state, ok := decodeJSON[DesiredState](w, request)
		if !ok {
			return
		}
		if !requireResourceOperation(
			w, request, governance, principal, configuration,
			environmentRID(state.Environment.ID), OperationProposeChange,
		) {
			return
		}
		if err := enrichRequiredArtifactAssessments(request.Context(), configuration.ArtifactScanner, &state, time.Now()); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		record, changeRequest, err := governance.ProposePlan(
			request.Context(), state, principal, request.Header.Get("X-Change-Title"), request.Header.Get("X-Change-Description"),
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		releaseCompatibilityAPI.syncDesiredState(request.Context(), state)
		w.Header().Set("X-Change-Request-ID", changeRequest.ID)
		writeJSON(w, http.StatusCreated, redactLeaseToken(record))
	})
	mux.HandleFunc("POST /v1/adapted-plans/{adapter}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		adapter, exists := configuration.EntityConfigurationAdapters[request.PathValue("adapter")]
		if !exists {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "configuration adapter not found"})
			return
		}
		body, ok := decodeJSONWithLimit[struct {
			DesiredState   DesiredState `json:"desiredState"`
			Configurations []struct {
				Entity             string              `json:"entity"`
				Mode               string              `json:"mode"`
				Payload            json.RawMessage     `json:"payload"`
				RequiredSecretKeys map[string][]string `json:"requiredSecretKeys,omitempty"`
			} `json:"configurations"`
		}](w, request, 4<<20)
		if !ok {
			return
		}
		if !requireResourceOperation(
			w, request, governance, principal, configuration,
			environmentRID(body.DesiredState.Environment.ID), OperationProposeChange,
		) {
			return
		}
		entities := make(map[string]*Entity, len(body.DesiredState.Entities))
		for index := range body.DesiredState.Entities {
			entity := &body.DesiredState.Entities[index]
			entities[entity.Name] = entity
		}
		adapted := make(map[string]struct{}, len(body.Configurations))
		for index, item := range body.Configurations {
			entity, found := entities[item.Entity]
			if !found {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("configurations[%d] references unknown Entity %q", index, item.Entity)})
				return
			}
			if _, duplicate := adapted[item.Entity]; duplicate {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("configurations[%d] duplicates Entity %q", index, item.Entity)})
				return
			}
			adapted[item.Entity] = struct{}{}
			managed, universal, requirements, err := adapter(item.Mode, item.Payload, item.RequiredSecretKeys)
			if err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("configurations[%d]: %v", index, err)})
				return
			}
			entity.Configuration, entity.Universal, entity.SecretRequirements = managed, universal, requirements
		}
		if err := enrichRequiredArtifactAssessments(request.Context(), configuration.ArtifactScanner, &body.DesiredState, time.Now()); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		record, changeRequest, err := governance.ProposePlan(
			request.Context(), body.DesiredState, principal,
			request.Header.Get("X-Change-Title"), request.Header.Get("X-Change-Description"),
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		releaseCompatibilityAPI.syncDesiredState(request.Context(), body.DesiredState)
		w.Header().Set("X-Change-Request-ID", changeRequest.ID)
		writeJSON(w, http.StatusCreated, redactLeaseToken(record))
	})
	mux.HandleFunc("POST /v1/commands", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[IssueCommandRequest](w, request)
		if !ok {
			return
		}
		if !requireResourceOperation(w, request, governance, principal, configuration,
			environmentRID(body.DesiredState.Environment.ID), OperationOperate) {
			return
		}
		if err := enrichRequiredArtifactAssessments(request.Context(), configuration.ArtifactScanner, &body.DesiredState, time.Now()); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		record, err := lifecycle.IssueCommand(request.Context(), body, principal.Username)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		releaseCompatibilityAPI.syncDesiredState(request.Context(), body.DesiredState)
		writeJSON(w, http.StatusCreated, redactLeaseToken(record))
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/commands", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration,
			environmentRID(request.PathValue("environmentID")), OperationReadResource) {
			return
		}
		plans, err := repository.ListPlans(request.Context(), request.PathValue("environmentID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		commands := make([]PlanRecord, 0)
		for _, record := range plans {
			if record.Command != nil {
				commands = append(commands, redactLeaseToken(record))
			}
		}
		writeJSON(w, http.StatusOK, commands)
	})
	mux.HandleFunc("GET /v1/teams", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := requireOperatorPrincipal(w, request, governance, configuration); !ok {
			return
		}
		teams, err := repository.ListTeams(request.Context())
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
	})
	mux.HandleFunc("GET /v1/teams/{teamName}", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := requireOperatorPrincipal(w, request, governance, configuration); !ok {
			return
		}
		team, err := repository.GetTeam(request.Context(), request.PathValue("teamName"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, team)
	})
	mux.HandleFunc("PUT /v1/teams/{teamName}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		if !requireTeamAdministrator(w, request, repository, principal, configuration) {
			return
		}
		team, ok := decodeJSON[Team](w, request)
		if !ok {
			return
		}
		if team.Name != "" && team.Name != request.PathValue("teamName") {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "team name must match the path"})
			return
		}
		team.Name = request.PathValue("teamName")
		previousLabels := map[string]string(nil)
		if existing, err := repository.GetTeam(request.Context(), team.Name); err == nil {
			previousLabels = existing.Labels
		} else if !errors.Is(err, ErrNotFound) {
			writeLifecycleError(w, err)
			return
		}
		if err := governance.ValidateLabelContributions(request.Context(), principal, previousLabels, team.Labels); err != nil {
			writeLifecycleError(w, err)
			return
		}
		stored, err := governance.PutTeam(request.Context(), team)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, stored)
	})
	mux.HandleFunc("POST /v1/resource-grants", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		grant, ok := decodeJSON[ResourceGrant](w, request)
		if !ok {
			return
		}
		if !requireResourceOperation(w, request, governance, principal, configuration, grant.ResourceRID, OperationManagePermissions) {
			return
		}
		stored, err := governance.PutGrantAs(request.Context(), grant, principal.Username)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, stored)
	})
	mux.HandleFunc("GET /v1/resource-grants", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := requireOperatorPrincipal(w, request, governance, configuration); !ok {
			return
		}
		grants, err := repository.ListResourceGrants(request.Context(), request.URL.Query().Get("resourceRid"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resourceGrants": grants})
	})
	mux.HandleFunc("PUT /v1/approval-policies/{policyID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		if !requireGovernanceAdministrator(w, request, governance, principal, configuration) {
			return
		}
		policy, ok := decodeJSON[ApprovalPolicy](w, request)
		if !ok {
			return
		}
		if policy.ID != "" && policy.ID != request.PathValue("policyID") {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "policy id must match the path"})
			return
		}
		policy.ID = request.PathValue("policyID")
		stored, err := governance.PutPolicy(request.Context(), policy)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, stored)
	})
	mux.HandleFunc("GET /v1/approval-policies", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := requireOperatorPrincipal(w, request, governance, configuration); !ok {
			return
		}
		policies, err := repository.ListApprovalPolicies(request.Context())
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"approvalPolicies": policies})
	})
	mux.HandleFunc("GET /v1/change-requests", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		query := request.URL.Query()
		cursor, limit, err := parsePageQuery(query, defaultPageLimit, maxPageLimit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		requests, err := repository.ListChangeRequests(request.Context(), "")
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		visible := requests[:0]
		for _, changeRequest := range requests {
			if canReadChangeRequest(request.Context(), governance, principal, configuration, changeRequest) {
				visible = append(visible, changeRequest)
			}
		}
		page, err := paginateChangeRequests(visible, ChangeRequestQuery{
			Environment: query.Get("environment"), Status: query.Get("status"), Search: query.Get("search"),
		}, cursor, limit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /v1/change-requests/{changeRequestID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		stored, err := repository.GetChangeRequest(request.Context(), request.PathValue("changeRequestID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if !canReadChangeRequest(request.Context(), governance, principal, configuration, stored) {
			writeLifecycleError(w, ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, annotateSatisfiability(request.Context(), repository, stored))
	})
	mux.HandleFunc("POST /v1/change-requests/{changeRequestID}/approve", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Rationale string `json:"rationale"`
		}](w, request)
		if !ok {
			return
		}
		stored, plan, err := governance.Approve(request.Context(), request.PathValue("changeRequestID"), principal, body.Rationale)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		result := map[string]any{"changeRequest": stored}
		if stored.PlanID != "" {
			result["plan"] = redactLeaseToken(plan)
		}
		if imported, err := finalizeApprovedChangeRequest(request.Context(), bundles, stored, true); err != nil {
			writeLifecycleError(w, err)
			return
		} else if imported != nil {
			result["bundleImport"] = imported
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/change-requests/{changeRequestID}/reject", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Rationale string `json:"rationale"`
		}](w, request)
		if !ok {
			return
		}
		stored, plan, err := governance.Reject(request.Context(), request.PathValue("changeRequestID"), principal, body.Rationale)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		result := map[string]any{"changeRequest": stored}
		if stored.PlanID != "" {
			result["plan"] = redactLeaseToken(plan)
		}
		if imported, err := finalizeApprovedChangeRequest(request.Context(), bundles, stored, false); err != nil {
			writeLifecycleError(w, err)
			return
		} else if imported != nil {
			result["bundleImport"] = imported
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/change-requests/{changeRequestID}/reopen", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		stored, plan, err := governance.Reopen(request.Context(), request.PathValue("changeRequestID"), principal)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		result := map[string]any{"changeRequest": stored}
		if stored.PlanID != "" {
			result["plan"] = redactLeaseToken(plan)
		}
		if imported, err := finalizeApprovedChangeRequest(request.Context(), bundles, stored, true); err != nil {
			writeLifecycleError(w, err)
			return
		} else if imported != nil {
			result["bundleImport"] = imported
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/change-requests/{changeRequestID}/cancel", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Reason string `json:"reason"`
		}](w, request)
		if !ok {
			return
		}
		candidate, err := repository.GetChangeRequest(request.Context(), request.PathValue("changeRequestID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if candidate.Author.Username != principal.Username {
			writeLifecycleError(w, ErrForbidden)
			return
		}
		if candidate.Status != ChangePendingApprovals && candidate.Status != ChangeApproved {
			writeLifecycleError(w, ErrInvalidTransition)
			return
		}
		var plan PlanRecord
		if candidate.PlanID != "" {
			plan, err = lifecycle.CancelPlan(request.Context(), candidate.PlanID, principal.Username, body.Reason)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
		}
		stored, err := governance.Cancel(request.Context(), candidate.ID, principal, body.Reason)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		result := map[string]any{"changeRequest": stored}
		if stored.PlanID != "" {
			result["plan"] = redactLeaseToken(plan)
		}
		if imported, err := finalizeApprovedChangeRequest(request.Context(), bundles, stored, false); err != nil {
			writeLifecycleError(w, err)
			return
		} else if imported != nil {
			result["bundleImport"] = imported
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("PUT /v1/change-requests/{changeRequestID}/labels", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Labels []string `json:"labels"`
		}](w, request)
		if !ok {
			return
		}
		stored, plan, approvedByThisCall, err := governance.SetLabels(request.Context(), request.PathValue("changeRequestID"), principal, body.Labels)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		result := map[string]any{"changeRequest": stored}
		if stored.PlanID != "" {
			result["plan"] = redactLeaseToken(plan)
		}
		if imported, err := finalizeApprovedChangeRequest(request.Context(), bundles, stored, approvedByThisCall); err != nil {
			writeLifecycleError(w, err)
			return
		} else if imported != nil {
			result["bundleImport"] = imported
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /v1/authorization-audit", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		if !requireGovernanceAdministrator(w, request, governance, principal, configuration) {
			return
		}
		events, err := repository.ListAuthorizationAudit(request.Context(), request.URL.Query().Get("resourceRid"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	})
	registerPromotionStageControl := func(pattern string, paused bool) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, request *http.Request) {
			principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
			if !ok || !requireResourceOperation(
				w, request, governance, principal, configuration, productRID(request.PathValue("product")), OperationOperate,
			) {
				return
			}
			body, ok := decodeJSON[struct {
				Rationale string `json:"rationale"`
			}](w, request)
			if !ok {
				return
			}
			control, err := lifecycle.SetPromotionStagePaused(
				request.Context(), request.PathValue("product"), request.PathValue("fromChannel"), request.PathValue("toChannel"),
				principal.Username, body.Rationale, paused,
			)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, control)
		})
	}
	registerPromotionStageControl(
		"POST /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}/pause", true,
	)
	registerPromotionStageControl(
		"POST /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}/resume", false,
	)
	mux.HandleFunc("GET /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requireResourceOperation(
			w, request, governance, principal, configuration, productRID(request.PathValue("product")), OperationReadResource,
		) {
			return
		}
		control, err := repository.GetPromotionStageControl(
			request.Context(), request.PathValue("product"), request.PathValue("fromChannel"), request.PathValue("toChannel"),
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, control)
	})
	mux.HandleFunc("POST /v1/promotions/evaluations", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationOperate) {
			return
		}
		body, ok := decodeJSON[PromotionEvaluationRequest](w, request)
		if !ok {
			return
		}
		record, err := lifecycle.EvaluatePromotion(request.Context(), body)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, record)
	})
	mux.HandleFunc("GET /v1/promotions/{promotionID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		record, err := repository.GetPromotion(request.Context(), request.PathValue("promotionID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("GET /v1/promotions", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok || !requirePlatformOperation(w, request, governance, principal, configuration, OperationReadResource) {
			return
		}
		query := request.URL.Query()
		cursor, limit, err := parsePageQuery(query, defaultPageLimit, maxPageLimit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		records, err := repository.ListPromotions(request.Context(), "")
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		page, err := paginatePromotions(records, PromotionQuery{
			Product: query.Get("product"), Status: query.Get("status"), Search: query.Get("search"),
		}, cursor, limit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	// Plans are readable one at a time already; a console needs the index.
	// Authorization is per record rather than per request, because a Plan is
	// readable either through the Change Request that produced it or through
	// the Environment it targets, and a principal may hold one and not the
	// other. Filtering happens after that check, so a filter can never widen
	// what a principal can see.
	mux.HandleFunc("GET /v1/plans", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		query := request.URL.Query()
		view := strings.TrimSpace(query.Get("view"))
		if view == "" {
			view = "full"
		}
		if view != "full" && view != "summary" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "view must be full or summary"})
			return
		}
		maximumLimit := fullCatalogPageLimit
		defaultLimit := fullCatalogPageLimit
		if view == "summary" {
			maximumLimit = maxPageLimit
			defaultLimit = defaultPageLimit
		}
		cursor, limit, err := parsePageQuery(query, defaultLimit, maximumLimit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		records, err := repository.ListPlans(request.Context(), "")
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		visible := make([]PlanRecord, 0, len(records))
		for _, record := range records {
			if !canReadPlan(request.Context(), repository, governance, principal, configuration, record) {
				continue
			}
			visible = append(visible, redactLeaseToken(record))
		}
		page, err := paginatePlans(visible, PlanQuery{
			Environment:    strings.TrimSpace(query.Get("environment")),
			Status:         strings.TrimSpace(query.Get("status")),
			InstallationID: strings.TrimSpace(query.Get("installationId")),
			Search:         query.Get("search"), View: view,
		}, cursor, limit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		if view == "summary" {
			writeJSON(w, http.StatusOK, summarizePlanPage(page))
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /v1/plans/{planID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		record, err := repository.GetPlan(request.Context(), request.PathValue("planID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if record.ChangeRequestID != "" {
			changeRequest, err := repository.GetChangeRequest(request.Context(), record.ChangeRequestID)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			if !canReadChangeRequest(request.Context(), governance, principal, configuration, changeRequest) {
				writeLifecycleError(w, ErrForbidden)
				return
			}
		} else if !requireResourceOperation(w, request, governance, principal, configuration, environmentRID(record.Plan.Environment), OperationReadResource) {
			return
		}
		writeJSON(w, http.StatusOK, redactLeaseToken(record))
	})
	mux.HandleFunc("POST /v1/plans/{planID}/approvals", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Rationale string `json:"rationale"`
		}](w, request)
		if !ok {
			return
		}
		existing, err := repository.GetPlan(request.Context(), request.PathValue("planID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		var record PlanRecord
		if existing.ChangeRequestID != "" {
			_, record, err = governance.Approve(request.Context(), existing.ChangeRequestID, principal, body.Rationale)
		} else {
			// A Plan with no Change Request is approved by Lifecycle.ApprovePlan,
			// which performs no eligibility check of any kind: no requirement, no
			// grant, only a per-subject dedupe. Authentication alone therefore let
			// any operator approve such a Plan, in any Environment, including one
			// they hold nothing on.
			//
			// The check is the one the GET route immediately above already makes,
			// raised to Operate because approving is an act rather than a read.
			// It applies to this branch only: the governed branch answers to the
			// reviewer requirements policy raised, and a reviewer may satisfy
			// those through a role or an operation grant without holding the
			// Environment itself -- requiring it here as well would refuse
			// reviewers the policy deliberately admitted.
			//
			// Refusing the branch outright would have been wrong: Change
			// Request-less Plans are legitimately created by the reconciliation
			// planner and by the command path, so this is a Plan that governance
			// never saw rather than a Plan that is broken.
			if !requireResourceOperation(w, request, governance, principal, configuration,
				environmentRID(existing.Plan.Environment), OperationOperate) {
				return
			}
			record, err = lifecycle.ApprovePlan(request.Context(), existing.ID, principal.Username, body.Rationale)
		}
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, redactLeaseToken(record))
	})
	mux.HandleFunc("POST /v1/plans/{planID}/cancel", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Reason                     string `json:"reason"`
			CreateSuppression          bool   `json:"createSuppression,omitempty"`
			SuppressionDurationSeconds int    `json:"suppressionDurationSeconds,omitempty"`
			InstallationID             string `json:"installationId,omitempty"`
		}](w, request)
		if !ok {
			return
		}
		subject := principal.Username
		existing, err := repository.GetPlan(request.Context(), request.PathValue("planID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if existing.ChangeRequestID != "" {
			changeRequest, err := repository.GetChangeRequest(request.Context(), existing.ChangeRequestID)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			if changeRequest.Author.Username != principal.Username {
				writeLifecycleError(w, ErrForbidden)
				return
			}
		} else {
			// A Plan with no Change Request is cancelled by Lifecycle.CancelPlan,
			// which performs no eligibility check: authentication alone therefore
			// let any operator cancel such a Plan, in any Environment, including
			// one they hold nothing on, and optionally plant a suppression.
			//
			// The check is the one the approval route for the same ungoverned
			// branch already makes. It applies to this branch only: a governed
			// Plan answers to its author, and requiring Operate here as well
			// would refuse an author who may cancel their own Change Request
			// without holding the Environment itself.
			if !requireResourceOperation(w, request, governance, principal, configuration,
				environmentRID(existing.Plan.Environment), OperationOperate) {
				return
			}
		}
		duration := body.SuppressionDurationSeconds
		if body.CreateSuppression {
			// A suppression is durable Environment state that outlives the Plan
			// which planted it, and POST /v1/environments/{id}/suppressions
			// gates that same effect at Operate. Authorship is standing over a
			// Change Request and not over an Environment, so the governed branch
			// above -- which asks only whether the author is calling -- let an
			// author who never held more than ProposeChange reach the effect at
			// the lower of two bars (#150).
			//
			// Only the suppression is gated and not the cancellation: requiring
			// Operate for the whole route would refuse an author cancelling
			// their own Change Request in an Environment they hold nothing on,
			// which governance admits on purpose. Stated once, after the branch
			// dispatch, so that both branches answer to it.
			if !governance.AuthorizeResourceOperation(request.Context(), principal, configuration,
				environmentRID(existing.Plan.Environment), OperationOperate) {
				writeLifecycleError(w, fmt.Errorf("%w: creating a suppression through Plan cancellation requires %s on %s",
					ErrForbidden, OperationOperate, environmentRID(existing.Plan.Environment)))
				return
			}
			if duration == 0 {
				duration = 30 * 60
			}
			if duration < 60 || duration > 30*24*60*60 {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
					"error": "suppression duration must be between one minute and 30 days",
				})
				return
			}
			if body.InstallationID != "" && !existing.Plan.ContainsInstallation(body.InstallationID) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "installation is not in the Plan"})
				return
			}
		}
		record, err := lifecycle.CancelPlan(request.Context(), request.PathValue("planID"), subject, body.Reason)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if record.ChangeRequestID != "" {
			if _, err := governance.Cancel(request.Context(), record.ChangeRequestID, principal, body.Reason); err != nil {
				writeLifecycleError(w, err)
				return
			}
		}
		if body.CreateSuppression {
			startsAt := time.Now().UTC()
			endsAt := startsAt.Add(time.Duration(duration) * time.Second)
			if _, err := lifecycle.CreateManualSuppression(
				request.Context(), record.Plan.Environment, body.InstallationID,
				"Plan cancellation: "+body.Reason, subject, startsAt, &endsAt,
			); err != nil {
				writeLifecycleError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, redactLeaseToken(record))
	})
	mux.HandleFunc("POST /v1/plans/{planID}/undo-unmanage", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[struct {
			Reason string `json:"reason"`
		}](w, request)
		if !ok {
			return
		}
		existing, err := repository.GetPlan(request.Context(), request.PathValue("planID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if !requireResourceOperation(w, request, governance, principal, configuration,
			environmentRID(existing.Plan.Environment), OperationOperate) {
			return
		}
		if existing.ChangeRequestID != "" {
			changeRequest, err := repository.GetChangeRequest(request.Context(), existing.ChangeRequestID)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			if changeRequest.Author.Username != principal.Username {
				writeLifecycleError(w, ErrForbidden)
				return
			}
		}
		record, err := lifecycle.UndoUnmanage(request.Context(), existing.ID, principal.Username, body.Reason)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if record.ChangeRequestID != "" {
			if _, err := governance.Cancel(request.Context(), record.ChangeRequestID, principal, body.Reason); err != nil {
				writeLifecycleError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, redactLeaseToken(record))
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/suppressions", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(
			w, request, governance, principal, configuration, environmentRID(environment), OperationReadResource,
		) {
			return
		}
		records, err := repository.ListSuppressions(request.Context(), environment)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
	mux.HandleFunc("POST /v1/environments/{environmentID}/suppressions", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(
			w, request, governance, principal, configuration, environmentRID(environment), OperationOperate,
		) {
			return
		}
		body, ok := decodeJSON[struct {
			InstallationID string     `json:"installationId,omitempty"`
			Reason         string     `json:"reason"`
			StartsAt       time.Time  `json:"startsAt,omitempty"`
			EndsAt         *time.Time `json:"endsAt,omitempty"`
		}](w, request)
		if !ok {
			return
		}
		record, err := lifecycle.CreateManualSuppression(
			request.Context(), environment, body.InstallationID,
			body.Reason, principal.Username, body.StartsAt, body.EndsAt,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, record)
	})
	mux.HandleFunc("DELETE /v1/suppressions/{suppressionID}", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		existing, err := repository.GetSuppression(request.Context(), request.PathValue("suppressionID"))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		if !requireResourceOperation(
			w, request, governance, principal, configuration, environmentRID(existing.Environment), OperationOperate,
		) {
			return
		}
		record, err := lifecycle.RemoveSuppression(
			request.Context(), existing.ID, principal.Username,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("POST /v1/environments/{environmentID}/leases", func(w http.ResponseWriter, request *http.Request) {
		identity, ok := requireAgentIdentity(w, request, configuration.AllowInsecureAgentIdentityHeaders)
		if !ok {
			return
		}
		if identity.Environment != request.PathValue("environmentID") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "client identity is not authorized for this Environment"})
			return
		}
		body, ok := decodeJSON[struct {
			DurationSeconds int `json:"durationSeconds"`
		}](w, request)
		if !ok {
			return
		}
		if body.DurationSeconds == 0 {
			body.DurationSeconds = 60
		}
		record, err := lifecycle.LeaseNextPlan(
			request.Context(), request.PathValue("environmentID"), identity.AgentID,
			time.Duration(body.DurationSeconds)*time.Second,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/health-targets", func(w http.ResponseWriter, request *http.Request) {
		identity, ok := requireAgentIdentity(w, request, configuration.AllowInsecureAgentIdentityHeaders)
		if !ok {
			return
		}
		environment := request.PathValue("environmentID")
		if identity.Environment != environment {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "client identity is not authorized for this Environment"})
			return
		}
		targets, err := latestHealthTargets(request.Context(), repository, environment)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
	})
	mux.HandleFunc("POST /v1/environments/{environmentID}/health-observations", func(w http.ResponseWriter, request *http.Request) {
		identity, ok := requireAgentIdentity(w, request, configuration.AllowInsecureAgentIdentityHeaders)
		if !ok {
			return
		}
		environment := request.PathValue("environmentID")
		if identity.Environment != environment {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "client identity is not authorized for this Environment"})
			return
		}
		body, ok := decodeJSON[struct {
			Observations []HealthObservation `json:"observations"`
		}](w, request)
		if !ok {
			return
		}
		report, err := applyHealthObservations(request.Context(), repository, environment, identity.AgentID, body.Observations, time.Now())
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
	mux.HandleFunc("POST /v1/plans/{planID}/start", func(w http.ResponseWriter, request *http.Request) {
		if !authorizeAgentForPlan(w, request, repository, configuration.AllowInsecureAgentIdentityHeaders) {
			return
		}
		if !requireEmptyBody(w, request) {
			return
		}
		record, err := lifecycle.StartPlan(request.Context(), request.PathValue("planID"), leaseToken(request))
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, redactLeaseToken(record))
	})
	mux.HandleFunc("POST /v1/plans/{planID}/heartbeat", func(w http.ResponseWriter, request *http.Request) {
		if !authorizeAgentForPlan(w, request, repository, configuration.AllowInsecureAgentIdentityHeaders) {
			return
		}
		body, ok := decodeJSON[struct {
			DurationSeconds int `json:"durationSeconds"`
		}](w, request)
		if !ok {
			return
		}
		if body.DurationSeconds == 0 {
			body.DurationSeconds = 60
		}
		record, err := lifecycle.RenewLease(
			request.Context(), request.PathValue("planID"), leaseToken(request),
			time.Duration(body.DurationSeconds)*time.Second,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, redactLeaseToken(record))
	})
	mux.HandleFunc("POST /v1/plans/{planID}/finish", func(w http.ResponseWriter, request *http.Request) {
		if !authorizeAgentForPlan(w, request, repository, configuration.AllowInsecureAgentIdentityHeaders) {
			return
		}
		body, ok := decodeJSON[struct {
			Status               PlanStatus       `json:"status"`
			Failure              string           `json:"failure,omitempty"`
			FailedInstallationID string           `json:"failedInstallationId,omitempty"`
			Execution            *PlanExecution   `json:"execution,omitempty"`
			Entities             []ReportedEntity `json:"entities,omitempty"`
			Secrets              []ReportedSecret `json:"secrets,omitempty"`
		}](w, request)
		if !ok {
			return
		}
		if body.Status != PlanSucceeded && body.Status != PlanFailed {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "status must be SUCCEEDED or FAILED"})
			return
		}
		report := &EnvironmentReport{Entities: body.Entities, Secrets: body.Secrets}
		record, err := lifecycle.FinishPlan(
			request.Context(), request.PathValue("planID"), leaseToken(request),
			body.Status == PlanSucceeded, body.Failure, body.FailedInstallationID, body.Execution, report,
		)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, redactLeaseToken(record))
	})
	// The Report names every installation, its release and config digests, the
	// namespaces, and each secret's provider reference, Kubernetes secret name
	// and key names. That is the fleet this Hub governs, so it belongs behind
	// the same door as GET /v1/environments/{environmentID} above, which reads a
	// superset of it. This route shipped with no principal and no resource
	// check: not as a deliberate exemption -- the open routes are the stateless
	// validators, which disclose only what the caller already sent -- but
	// because nothing tested it.
	mux.HandleFunc("GET /v1/environments/{environmentID}/reported-state", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		environment := request.PathValue("environmentID")
		if !ok || !requireResourceOperation(w, request, governance, principal, configuration, environmentRID(environment), OperationReadResource) {
			return
		}
		report, err := repository.GetEnvironmentReport(request.Context(), environment)
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
	mux.HandleFunc("GET /v1/fleet/installations", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		query := request.URL.Query()
		maxAge := time.Duration(0)
		if raw := query.Get("maxAgeSeconds"); raw != "" {
			seconds, err := strconv.Atoi(raw)
			if err != nil || seconds < 1 || seconds > 86400 {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "maxAgeSeconds must be between 1 and 86400"})
				return
			}
			maxAge = time.Duration(seconds) * time.Second
		}
		cursor, limit, err := parsePageQuery(query, defaultPageLimit, maxPageLimit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		fleetQuery := FleetQuery{
			Environment: query.Get("environment"), Product: query.Get("product"), Health: query.Get("health"),
			Rollout: query.Get("rolloutStatus"), Config: query.Get("configStatus"),
			Lifecycle: query.Get("lifecycle"), Search: query.Get("search"), MaxAge: maxAge,
		}
		projection, err := BuildFleetProjection(request.Context(), repository, fleetQuery, time.Now())
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		visible := make([]FleetInstallation, 0, len(projection.Installations))
		projection.Summary = FleetSummary{Health: map[string]int{}, Rollout: map[string]int{}, Config: map[string]int{}, Lifecycle: map[string]int{}}
		projection.MaxResultAgeSeconds = 0
		administrator := isGovernanceAdministrator(request.Context(), governance, principal, configuration)
		for _, item := range projection.Installations {
			if administrator || governance.Authorize(request.Context(), effectivePrincipal(principal), environmentRID(item.Environment), OperationReadResource) {
				visible = append(visible, item)
				addFleetSummary(&projection, item)
			}
		}
		projection.Installations = visible
		projection, err = paginateFleetProjection(projection, fleetQuery, cursor, limit)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, projection)
	})
	mux.HandleFunc("POST /v1/fleet/actions", func(w http.ResponseWriter, request *http.Request) {
		principal, ok := requireOperatorPrincipal(w, request, governance, configuration)
		if !ok {
			return
		}
		body, ok := decodeJSON[FleetActionRequest](w, request)
		if !ok {
			return
		}
		body.Action = strings.ToUpper(strings.TrimSpace(body.Action))
		body.Reason = strings.TrimSpace(body.Reason)
		if err := validateFleetAction(body); err != nil {
			writeLifecycleError(w, err)
			return
		}
		projection, err := BuildFleetProjection(request.Context(), repository, FleetQuery{}, time.Now())
		if err != nil {
			writeLifecycleError(w, err)
			return
		}
		installations := map[string]FleetInstallation{}
		for _, item := range projection.Installations {
			installations[reporting.Key(item.Environment, item.InstallationID)] = item
		}
		byEnvironment := map[string][]FleetActionTarget{}
		for _, target := range body.Targets {
			item, exists := installations[reporting.Key(target.Environment, target.InstallationID)]
			if !exists {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "unknown fleet installation " + target.Environment + "/" + target.InstallationID})
				return
			}
			if !requireBulkTargetOperation(w, request, governance, principal, configuration, item) {
				return
			}
			byEnvironment[target.Environment] = append(byEnvironment[target.Environment], target)
		}
		environments := make([]string, 0, len(byEnvironment))
		for environment := range byEnvironment {
			environments = append(environments, environment)
		}
		sort.Strings(environments)
		results := make([]FleetActionResult, 0, len(environments))
		for _, environment := range environments {
			targets := byEnvironment[environment]
			sort.Slice(targets, func(i, j int) bool { return targets[i].InstallationID < targets[j].InstallationID })
			changes := make([]EntityChange, 0, len(targets))
			for _, target := range targets {
				item := installations[reporting.Key(environment, target.InstallationID)]
				changes = append(changes, fleetActionChange(request.Context(), governance, effectivePrincipal(principal), body, item))
			}
			changeRequest, policyApproves, err := governance.ProposeChanges(
				request.Context(), "FLEET_ACTION", fleetActionTargetID(body, environment, targets), environment, changes,
				principal, body.Action+" "+strconv.Itoa(len(targets))+" fleet installation(s)", body.Reason,
			)
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			// Same rule as the sites above. The fleet action's own execution record
			// only refuses a re-run once one has succeeded, so a stale approval
			// whose execution failed is still executable -- and its resume path,
			// which re-runs the items that failed, is preserved because this asks
			// the current policy rather than whether the record is new.
			if policyApproves {
				changeRequest, err = executeApprovedFleetAction(request.Context(), repository, lifecycle, governance, changeRequest)
			}
			if err != nil {
				writeLifecycleError(w, err)
				return
			}
			results = append(results, FleetActionResult{Environment: environment, ChangeRequest: changeRequest})
		}
		writeJSON(w, http.StatusCreated, map[string]any{"results": results})
	})
	return secureResponseHeaders(mux)
}

func secureResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if request.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, request)
	})
}

// approvedByThisCall is whether the review that just ran is what approved the
// request, rather than the request having already been approved when the route
// was called. Only the export-pipeline branch needs it. The fleet branch is
// already idempotent -- executeApprovedFleetAction returns early on an
// execution that has succeeded -- and the Bundle import branch is a status
// sync that every review route is meant to perform whatever the status is.
//
// Approve and Reopen pass true: approveOnce admits only PENDING_APPROVALS, and
// Reopen re-evaluates the change against the policy in force, so an APPROVED
// result from either is a decision taken by this call. Reject and Cancel pass
// false and cannot produce APPROVED at all. Labelling is the one route that
// has to compute it, and the one that was replaying actions.
func finalizeApprovedChangeRequest(
	ctx context.Context, bundles *BundleService, request ChangeRequest, approvedByThisCall bool,
) (*BundleImport, error) {
	if request.TargetKind == "FLEET_ACTION" {
		_, err := executeApprovedFleetAction(ctx, bundles.repository, NewLifecycle(bundles.repository), NewGovernanceService(bundles.repository), request)
		return nil, err
	}
	// Archiving, unarchiving and deleting a pipeline are unconditional writes
	// with no record of having run, so a second call performs the action a
	// second time. Reaching them requires an approval taken now.
	if approvedByThisCall && request.Status == ChangeApproved && strings.HasPrefix(request.TargetKind, "EXPORT_PIPELINE_LIFECYCLE_") {
		switch strings.TrimPrefix(request.TargetKind, "EXPORT_PIPELINE_LIFECYCLE_") {
		case ChangeTypeArchivePipeline:
			_, err := bundles.SetPipelineArchived(ctx, request.TargetID, true)
			return nil, err
		case ChangeTypeUnarchivePipeline:
			_, err := bundles.SetPipelineArchived(ctx, request.TargetID, false)
			return nil, err
		case ChangeTypeDeletePipeline:
			_, err := bundles.DeletePipeline(ctx, request.TargetID)
			return nil, err
		}
	}
	if request.TargetKind != "BUNDLE_IMPORT" {
		return nil, nil
	}
	imported, err := bundles.SyncImportReviewStatus(ctx, request.TargetID, request.Status)
	if err != nil {
		return nil, err
	}
	return &imported, nil
}

func requireBulkTargetOperation(
	w http.ResponseWriter, request *http.Request, governance *GovernanceService, principal Principal,
	configuration HandlerOptions, item FleetInstallation,
) bool {
	effective := effectivePrincipal(principal)
	environment := environmentRID(item.Environment)
	installation := installationRID(item.Environment, item.InstallationID)
	authorized := isGovernanceAdministrator(request.Context(), governance, principal, configuration) ||
		(governance.Authorize(request.Context(), effective, environment, OperationBulkChange) &&
			(governance.Authorize(request.Context(), effective, installation, OperationOperate) ||
				governance.Authorize(request.Context(), effective, environment, OperationOperate)))
	decision, reason := "ALLOW", "bulk target individually authorized"
	if !authorized {
		decision, reason = "DENY", "missing fleet bulk-change and resource operate authority"
	}
	governance.audit(request.Context(), principal.Username, OperationBulkChange, installation, decision, reason, map[string]any{"environment": item.Environment})
	if !authorized {
		writeLifecycleError(w, ErrForbidden)
	}
	return authorized
}

func fleetActionChange(
	ctx context.Context, governance *GovernanceService, author Principal, request FleetActionRequest, item FleetInstallation,
) EntityChange {
	resourceRID := installationRID(item.Environment, item.InstallationID)
	change := EntityChange{
		ID: "chg_" + stableShortID(request.Action+"\x00"+resourceRID), Type: request.Action,
		InstallationID: item.InstallationID,
		Request:        map[string]any{"reason": request.Reason, "durationSeconds": request.DurationSeconds},
		Diff:           map[string]ChangeDiffEntry{"suppressed": {}},
		ReferencedResources: map[string]ReferencedResource{
			"environment":  governance.referencedResource(ctx, environmentRID(item.Environment), item.Environment, author),
			"product":      governance.referencedResource(ctx, productRID(item.Product), item.Product, author),
			"installation": governance.referencedResource(ctx, resourceRID, item.InstallationID, author),
		},
	}
	if request.Action == BulkSuppress {
		change.Diff["suppressed"] = ChangeDiffEntry{Before: false, After: true}
	} else {
		change.Diff["suppressed"] = ChangeDiffEntry{Before: true, After: false}
	}
	return change
}

func addFleetSummary(projection *FleetProjection, item FleetInstallation) {
	projection.Summary.Total++
	projection.Summary.Health[item.Health]++
	projection.Summary.Rollout[item.RolloutStatus]++
	projection.Summary.Config[item.ConfigStatus]++
	projection.Summary.Lifecycle[item.Lifecycle]++
	if item.Stale {
		projection.Summary.Stale++
	}
	if item.ObservationAgeSeconds > projection.MaxResultAgeSeconds {
		projection.MaxResultAgeSeconds = item.ObservationAgeSeconds
	}
}

func requireAgentIdentity(w http.ResponseWriter, request *http.Request, allowInsecureHeaders bool) (AgentIdentity, bool) {
	identity, err := httpidentity.AgentIdentityFromRequest(request, allowInsecureHeaders)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return AgentIdentity{}, false
	}
	return identity, true
}

func authorizeAgentForPlan(
	w http.ResponseWriter,
	request *http.Request,
	repository Repository,
	allowInsecureHeaders bool,
) bool {
	identity, ok := requireAgentIdentity(w, request, allowInsecureHeaders)
	if !ok {
		return false
	}
	record, err := repository.GetPlan(request.Context(), request.PathValue("planID"))
	if err != nil {
		writeLifecycleError(w, err)
		return false
	}
	if identity.Environment != record.Plan.Environment || !execution.HeldByAgent(record.Lease, identity.AgentID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "client identity is not authorized for this Plan lease"})
		return false
	}
	return true
}

func decodeJSON[T any](w http.ResponseWriter, request *http.Request) (T, bool) {
	return decodeJSONWithLimit[T](w, request, 1<<20)
}

// decodeJSONWithLimit reports an over-limit body as 413 rather than folding it
// into the 400 that a malformed document gets. A client that trips the bound is
// sending well-formed JSON and needs to know the size is what was refused.
func decodeJSONWithLimit[T any](w http.ResponseWriter, request *http.Request, maximum int64) (T, bool) {
	request.Body = http.MaxBytesReader(w, request.Body, maximum)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request exceeds payload limit"})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		}
		return value, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request must contain exactly one JSON document"})
		return value, false
	}
	return value, true
}

func requireEmptyBody(w http.ResponseWriter, request *http.Request) bool {
	request.Body = http.MaxBytesReader(w, request.Body, 1)
	buffer := make([]byte, 1)
	count, err := request.Body.Read(buffer)
	if count != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be empty"})
		return false
	}
	return true
}

func leaseToken(request *http.Request) string {
	return strings.TrimSpace(request.Header.Get("X-Platform-Lease-Token"))
}

func redactLeaseToken(record PlanRecord) PlanRecord {
	if record.Lease != nil {
		lease := *record.Lease
		lease.Token = ""
		record.Lease = &lease
	}
	return record
}

func requireOperatorPrincipal(
	w http.ResponseWriter, request *http.Request, governance *GovernanceService, configuration HandlerOptions,
) (Principal, bool) {
	var claims IdentityClaims
	var err error
	if configuration.OperatorIdentityResolver != nil {
		claims, err = configuration.OperatorIdentityResolver(request)
	} else if configuration.AllowInsecureOperatorIdentityHeaders {
		claims = IdentityClaims{
			Username:       request.Header.Get("X-Platform-Subject"),
			Groups:         strings.Split(request.Header.Get("X-Platform-Groups"), ","),
			ServiceAccount: strings.EqualFold(request.Header.Get("X-Platform-Service-Account"), "true"),
			OnBehalfOf:     request.Header.Get("X-Platform-On-Behalf-Of"),
		}
		if strings.TrimSpace(claims.Username) == "" {
			claims.Username = "local-developer"
		}
	} else if request.TLS != nil && len(request.TLS.VerifiedChains) > 0 && len(request.TLS.VerifiedChains[0]) > 0 {
		claims, err = identity.OperatorIdentityFromCertificate(request.TLS.VerifiedChains[0][0])
	} else {
		err = ErrUnauthorized
	}
	if err != nil {
		writeLifecycleError(w, err)
		return Principal{}, false
	}
	principal, err := governance.ResolvePrincipal(request.Context(), claims)
	if err != nil {
		writeLifecycleError(w, err)
		return Principal{}, false
	}
	return principal, true
}

func requireGovernanceAdministrator(
	w http.ResponseWriter, request *http.Request, governance *GovernanceService, principal Principal, configuration HandlerOptions,
) bool {
	if isGovernanceAdministrator(request.Context(), governance, principal, configuration) {
		return true
	}
	writeLifecycleError(w, ErrForbidden)
	return false
}

func isGovernanceAdministrator(
	ctx context.Context, governance *GovernanceService, principal Principal, configuration HandlerOptions,
) bool {
	return governance.resourceDecision(configuration).Administrator(ctx, principal)
}

func requireResourceOperation(
	w http.ResponseWriter, request *http.Request, governance *GovernanceService, principal Principal,
	configuration HandlerOptions, resourceRID, operation string,
) bool {
	if governance.AuthorizeResourceOperation(request.Context(), principal, configuration, resourceRID, operation) {
		return true
	}
	writeLifecycleError(w, ErrForbidden)
	return false
}

func requirePlatformOperation(
	w http.ResponseWriter, request *http.Request, governance *GovernanceService, principal Principal,
	configuration HandlerOptions, operation string,
) bool {
	return requireResourceOperation(w, request, governance, principal, configuration, "platform/root", operation)
}

func requireModuleInstallationOperation(
	w http.ResponseWriter, request *http.Request, repository Repository, governance *GovernanceService,
	principal Principal, configuration HandlerOptions, operation string,
) bool {
	installation, err := repository.GetModuleInstallation(request.Context(), request.PathValue("installationID"))
	if err != nil {
		writeLifecycleError(w, err)
		return false
	}
	return requireResourceOperation(
		w, request, governance, principal, configuration, environmentRID(installation.Environment), operation,
	)
}

func canReadChangeRequest(
	ctx context.Context, governance *GovernanceService, principal Principal,
	configuration HandlerOptions, request ChangeRequest,
) bool {
	effective := effectivePrincipal(principal)
	return isGovernanceAdministrator(ctx, governance, principal, configuration) || request.Author.Username == principal.Username ||
		(request.Author.OnBehalfOf != nil && request.Author.OnBehalfOf.Username == effective.Username) ||
		principalCanReview(ctx, governance.repository, request.Requirements, effective) ||
		governance.Authorize(ctx, effective, environmentRID(request.Environment), OperationReadResource)
}

func requireTeamAdministrator(
	w http.ResponseWriter, request *http.Request, repository Repository, principal Principal, configuration HandlerOptions,
) bool {
	if contains(normalizedStrings(configuration.BootstrapAdministrators), principal.Username) ||
		(configuration.AllowInsecureOperatorIdentityHeaders && len(configuration.BootstrapAdministrators) == 0 && principal.Username == "local-developer") {
		return true
	}
	team, err := repository.GetTeam(request.Context(), request.PathValue("teamName"))
	if err == nil && (contains(team.AdminUsers, principal.Username) || intersects(stringSet(principal.Groups), team.AdminGroups)) {
		return true
	}
	writeLifecycleError(w, ErrForbidden)
	return false
}

func writeLifecycleError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var validationErrors ValidationErrors
	switch {
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrNoPlanAvailable):
		status = http.StatusNoContent
	case errors.Is(err, ErrConflict), errors.Is(err, ErrInvalidTransition):
		status = http.StatusConflict
	case errors.Is(err, ErrLeaseRejected):
		status = http.StatusForbidden
	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, ErrPlanCancelled):
		status = http.StatusGone
	case errors.Is(err, ErrDigestMismatch):
		status = http.StatusUnprocessableEntity
	case errors.As(err, &validationErrors), strings.Contains(err.Error(), "validation error"):
		status = http.StatusUnprocessableEntity
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, map[string]string{"error": err.Error(), "status": strconv.Itoa(status)})
}

const maximumArtifactUploadBytes = int64(8) << 30

func uploadArtifactBlob(w http.ResponseWriter, request *http.Request, repository ArtifactRepository, expectedDigest string) {
	request.Body = http.MaxBytesReader(w, request.Body, maximumArtifactUploadBytes)
	descriptor, created, err := repository.PutBlob(
		request.Context(), expectedDigest, request.Header.Get("Content-Type"), request.Body,
	)
	if err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "artifact exceeds upload limit"})
			return
		}
		writeLifecycleError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/artifacts/blobs/"+descriptor.Digest)
	w.Header().Set("Docker-Content-Digest", descriptor.Digest)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, descriptor)
}

func serveArtifactBlob(
	w http.ResponseWriter, request *http.Request, repository ArtifactRepository, includeBody bool,
) {
	descriptor, contents, err := repository.OpenBlob(request.Context(), request.PathValue("digest"))
	if err != nil {
		writeLifecycleError(w, err)
		return
	}
	defer contents.Close()
	w.Header().Set("Content-Type", descriptor.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(descriptor.Size, 10))
	w.Header().Set("Docker-Content-Digest", descriptor.Digest)
	w.Header().Set("ETag", `"`+descriptor.Digest+`"`)
	w.Header().Set("Cache-Control", "private, immutable")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.TrimPrefix(descriptor.Digest, "sha256:")+`"`)
	w.WriteHeader(http.StatusOK)
	if includeBody {
		_, _ = io.Copy(w, contents)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// canReadPlan mirrors the single-Plan read: a Plan produced by a Change
// Request is readable by whoever may read that Change Request, and one that
// was not is readable by whoever may read its Environment. A repository error
// denies rather than admits -- an index that fails open leaks every Plan the
// moment the Change Request store is unavailable.
func canReadPlan(
	ctx context.Context, repository Repository, governance *GovernanceService,
	principal Principal, configuration HandlerOptions, record PlanRecord,
) bool {
	if record.ChangeRequestID != "" {
		changeRequest, err := repository.GetChangeRequest(ctx, record.ChangeRequestID)
		if err != nil {
			return false
		}
		return canReadChangeRequest(ctx, governance, principal, configuration, changeRequest)
	}
	return isGovernanceAdministrator(ctx, governance, principal, configuration) ||
		governance.Authorize(ctx, effectivePrincipal(principal), environmentRID(record.Plan.Environment), OperationReadResource)
}

// planTargetsInstallation reports whether a Plan acts on an Installation, by
// its steps rather than its Environment: an Environment-wide Plan that does
// not touch the Installation being asked about is not an answer to that
// question.
func planTargetsInstallation(record PlanRecord, installationID string) bool {
	for _, step := range record.Plan.Steps {
		if step.InstallationID == installationID {
			return true
		}
	}
	return false
}
