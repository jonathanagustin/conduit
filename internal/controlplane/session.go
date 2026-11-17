package controlplane

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	maximumSessionResourceQueries = 32
	maximumSessionTeams           = 64
)

var sessionResourceRIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*){0,3}$`)

// sessionOperationVocabulary is Governance's list, not a second copy of it. The
// projection must offer exactly the operations Platform can act on, and a local
// copy would let the two drift into a session advertising a capability no
// policy could demand.
var sessionOperationVocabulary = OperationVocabulary

type SessionIdentity struct {
	Username       string   `json:"username"`
	Teams          []string `json:"teams"`
	TeamsTruncated bool     `json:"teamsTruncated"`
	ServiceAccount bool     `json:"serviceAccount"`
}

type SessionApplication struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type SessionResourceCapabilities struct {
	ResourceRID string   `json:"resourceRid"`
	Operations  []string `json:"operations"`
}

type SessionProjection struct {
	Actor            SessionIdentity               `json:"actor"`
	Effective        SessionIdentity               `json:"effective"`
	Delegated        bool                          `json:"delegated"`
	Applications     []SessionApplication          `json:"applications"`
	GlobalOperations []string                      `json:"globalOperations"`
	Resources        []SessionResourceCapabilities `json:"resources"`
}

func projectSession(
	ctx context.Context,
	governance *GovernanceService,
	principal Principal,
	configuration HandlerOptions,
	requestedResourceRIDs []string,
) (SessionProjection, error) {
	if len(requestedResourceRIDs) > maximumSessionResourceQueries {
		return SessionProjection{}, fmt.Errorf("at most %d resourceRid values are allowed", maximumSessionResourceQueries)
	}
	resourceRIDs := make([]string, 0, len(requestedResourceRIDs))
	seen := map[string]struct{}{}
	for _, raw := range requestedResourceRIDs {
		rid := strings.TrimSpace(raw)
		if len(rid) > 512 || !sessionResourceRIDPattern.MatchString(rid) {
			return SessionProjection{}, fmt.Errorf("resourceRid is not a bounded canonical resource coordinate")
		}
		if _, duplicate := seen[rid]; duplicate {
			continue
		}
		seen[rid] = struct{}{}
		resourceRIDs = append(resourceRIDs, rid)
	}
	sort.Strings(resourceRIDs)

	effective := effectivePrincipal(principal)
	globalOperations := sessionOperations(ctx, governance, principal, configuration, "platform/root")
	globalRead := contains(globalOperations, OperationReadResource)
	projection := SessionProjection{
		Actor:     projectSessionIdentity(principal),
		Effective: projectSessionIdentity(effective),
		Delegated: principal.OnBehalfOf != nil,
		Applications: []SessionApplication{
			{ID: "change-requests", Enabled: true},
			{ID: "fleet", Enabled: globalRead},
			{ID: "plans", Enabled: true},
			{ID: "promotions", Enabled: globalRead},
			// The console builds its Applications rail from this list, so a screen
			// absent here is unreachable through the navigation UI however complete
			// its route. Teams is readable by any operator (GET /v1/teams needs only
			// an operator principal); Risk, Export, and Import read platform-scoped
			// collections (catalog-retirements, export-pipelines, bundle-imports),
			// so they follow the same global-read gate fleet and promotions use.
			{ID: "teams", Enabled: true},
			{ID: "risk", Enabled: globalRead},
			{ID: "export", Enabled: globalRead},
			{ID: "import", Enabled: globalRead},
		},
		GlobalOperations: globalOperations,
		Resources:        make([]SessionResourceCapabilities, 0, len(resourceRIDs)),
	}
	for _, rid := range resourceRIDs {
		scope, known := rid, true
		var err error
		if configuration.SessionResourceScope != nil {
			scope, known, err = configuration.SessionResourceScope(ctx, rid)
		}
		operations := []string{}
		if err == nil && known && scope != "" {
			operations = sessionOperations(ctx, governance, principal, configuration, scope)
		}
		projection.Resources = append(projection.Resources, SessionResourceCapabilities{
			ResourceRID: rid,
			Operations:  operations,
		})
	}
	return projection, nil
}

func projectSessionIdentity(principal Principal) SessionIdentity {
	teams := append([]string(nil), principal.Teams...)
	truncated := len(teams) > maximumSessionTeams
	if truncated {
		teams = teams[:maximumSessionTeams]
	}
	if teams == nil {
		teams = []string{}
	}
	return SessionIdentity{
		Username:       principal.Username,
		Teams:          teams,
		TeamsTruncated: truncated,
		ServiceAccount: principal.ServiceAccount,
	}
}

func sessionOperations(
	ctx context.Context,
	governance *GovernanceService,
	principal Principal,
	configuration HandlerOptions,
	resourceRID string,
) []string {
	operations := make([]string, 0, len(sessionOperationVocabulary))
	administrator := isGovernanceAdministrator(ctx, governance, principal, configuration)
	effective := effectivePrincipal(principal)
	for _, operation := range sessionOperationVocabulary {
		if administrator || governance.Authorize(ctx, effective, resourceRID, operation) {
			operations = append(operations, operation)
		}
	}
	return operations
}
