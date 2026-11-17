package composition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationhttp"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	identityports "github.com/jonathanagustin/conduit/internal/identity/ports"
)

// Agent credential revocation and expiry visibility.
//
// A client certificate is valid until it expires or its CA is replaced, and
// before this file those were the only two ways to cut an Agent off. The Hub
// had no deny-list. guardAgentCredentials is that deny-list's enforcement, and
// it is one wrapper around the whole Hub rather than a call in each Agent
// handler for a reason that is specific to this codebase: the Agent routes are
// mounted by three mechanisms -- the legacy facade, the generated Apollo
// transport and the temporary-maintenance adapter -- and a check that has to
// be remembered at each of them is a check the fourth one will not make.
//
// Which requests it applies to is read from HubRouteAuthorization, the same
// register the unregistered-route guard and the limiter read. A route declared
// DecisionAgent is checked; a route added tomorrow with that decision is
// checked without anybody touching this file; and the route-authorization gate
// already refuses a route with no declared decision.

// agentSightingInterval is how often one replica rewrites an unchanged
// sighting. It bounds write load from a polling Agent and bounds how stale
// "last seen" can be. A changed certificate is written at once.
const agentSightingInterval = 5 * time.Minute

// agentRefusalLogInterval keeps a revoked Agent, which polls every few
// seconds, from writing a log line per poll.
const agentRefusalLogInterval = time.Minute

type agentCredentialGuard struct {
	store identityports.AgentCredentialStore
	now   func() time.Time
	log   *slog.Logger

	mutex    sync.Mutex
	recorded map[identity.AgentIdentity]agentSightingMark
	refused  map[identity.AgentIdentity]time.Time
}

type agentSightingMark struct {
	serial string
	at     time.Time
}

// guardAgentCredentials refuses a revoked Agent before any Agent handler runs.
//
// It fails closed, and only for Agents. A deny-list that cannot be read is an
// unknown deny-list, so every Agent request is answered 503 until it can be:
// admitting an Agent because the database was down would make an outage the
// way around a revocation. Operator, public and health routes never reach the
// store here, so the same outage does not take the operator surface down with
// it -- including the routes an operator would use to see what is wrong.
//
// A request that presents no usable Agent identity is passed through
// untouched. The handler beneath refuses it with the 401 it always has; this
// guard does not get to decide who somebody is, only whether a known somebody
// is still welcome.
func guardAgentCredentials(
	hub http.Handler, options controlplane.HandlerOptions, store identityports.AgentCredentialStore,
	now func() time.Time, logger *slog.Logger,
) http.Handler {
	routes := http.NewServeMux()
	for _, route := range HubRouteAuthorization() {
		routes.Handle(route.Pattern, routeDecision(route.Decision))
	}
	guard := &agentCredentialGuard{
		store: store, now: now, log: logger,
		recorded: map[identity.AgentIdentity]agentSightingMark{}, refused: map[identity.AgentIdentity]time.Time{},
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		matched, pattern := routes.Handler(request)
		if decision, registered := matched.(routeDecision); !registered || pattern == "" || Decision(decision) != DecisionAgent {
			hub.ServeHTTP(writer, request)
			return
		}
		credential, err := httpidentity.AgentCredentialFromRequest(request, options.AllowInsecureAgentIdentityHeaders)
		if err != nil {
			hub.ServeHTTP(writer, request)
			return
		}
		revocations, err := store.Revocations(request.Context(), credential.Identity.Environment)
		if err != nil {
			guard.log.ErrorContext(request.Context(), "Agent revocation state is unreadable; refusing Agent requests",
				"environment", credential.Identity.Environment, "agent", credential.Identity.AgentID, "error", err)
			writer.Header().Set("Retry-After", "5")
			writeAgentCredentialJSON(writer, http.StatusServiceUnavailable,
				map[string]string{"error": "Agent revocation state is unavailable; the request was not admitted"})
			return
		}
		if revocation, revoked := identity.RefusingRevocation(revocations, credential); revoked {
			guard.logRefusal(request.Context(), credential, revocation)
			writeAgentCredentialJSON(writer, http.StatusForbidden,
				map[string]string{"error": "Agent credential is revoked", "revocationId": revocation.ID})
			return
		}
		guard.sight(request.Context(), credential)
		hub.ServeHTTP(writer, request)
	})
}

func (guard *agentCredentialGuard) logRefusal(ctx context.Context, credential identity.AgentCredential, revocation identity.AgentRevocation) {
	now := guard.now()
	guard.mutex.Lock()
	last, seen := guard.refused[credential.Identity]
	quiet := seen && now.Sub(last) < agentRefusalLogInterval
	if !quiet {
		if len(guard.refused) >= 4096 {
			clear(guard.refused)
		}
		guard.refused[credential.Identity] = now
	}
	guard.mutex.Unlock()
	if quiet {
		return
	}
	guard.log.WarnContext(ctx, "refused a revoked Agent credential",
		"environment", credential.Identity.Environment, "agent", credential.Identity.AgentID,
		"serial", credential.Serial, "revocation", revocation.ID, "scope", revocation.Scope)
}

// sight records the certificate an admitted Agent presented. It is visibility,
// not a control, so a failure is logged and the request proceeds: refusing an
// Agent because a "last seen" row could not be written would turn a metric
// into an outage.
func (guard *agentCredentialGuard) sight(ctx context.Context, credential identity.AgentCredential) {
	if credential.Serial == "" {
		return
	}
	now := guard.now()
	guard.mutex.Lock()
	mark, seen := guard.recorded[credential.Identity]
	fresh := seen && mark.serial == credential.Serial && now.Sub(mark.at) < agentSightingInterval
	guard.mutex.Unlock()
	if fresh {
		return
	}
	err := guard.store.RecordSighting(ctx, identity.AgentCredentialSighting{
		Environment: credential.Identity.Environment, AgentID: credential.Identity.AgentID, Serial: credential.Serial,
		NotBefore: credential.NotBefore, NotAfter: credential.NotAfter, SeenAt: now.UTC(),
	})
	if err != nil {
		guard.log.WarnContext(ctx, "record Agent certificate sighting", "error", err)
		return
	}
	guard.mutex.Lock()
	if len(guard.recorded) >= 4096 {
		clear(guard.recorded)
	}
	guard.recorded[credential.Identity] = agentSightingMark{serial: credential.Serial, at: now}
	guard.mutex.Unlock()
}

// mountAgentCredentials binds the operator surface over the deny-list to this
// Hub's operator resolution, Governance and audit trail.
//
// Revoking an Agent is a change to who may act in an Environment, so it takes
// the authority a resource grant on that Environment takes --
// manage-permissions -- and reading the deny-list takes the Environment's read
// authority. Governance records the decision either way; auditAgentRevocation
// then records what the operator did with it.
func mountAgentCredentials(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions,
	store identityports.AgentCredentialStore, now func() time.Time,
) (http.Handler, error) {
	governance := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	err := revocationhttp.Register(mux, revocationhttp.Config{
		Store: store, Now: now,
		Authorize: func(request *http.Request, environment string, manage bool) (string, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return "", revocationhttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return "", revocationhttp.StatusError{Status: statusForIdentityError(err), Message: err.Error()}
			}
			operation := controlplane.OperationReadResource
			if manage {
				operation = controlplane.OperationManagePermissions
			}
			if !governance.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+environment, operation) {
				return "", revocationhttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
			}
			return principal.Username, nil
		},
		Audit: func(ctx context.Context, actor, action string, before, after *identity.AgentRevocation) error {
			return auditAgentRevocation(ctx, repository, now(), actor, action, before, after)
		},
		Remove: atomicAgentRevocationRemoval(store, repository, now),
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return mux, nil
}

// atomicAgentRevocationRemoval chooses an audit/removal boundary the store can
// actually uphold. A custom store without one keeps read/add support and its
// removal route fails closed; delete-and-compensate is never a fallback.
func atomicAgentRevocationRemoval(
	store identityports.AgentCredentialStore, repository controlplane.Repository, now func() time.Time,
) func(context.Context, string, string, string) (identity.AgentRevocation, error) {
	if durable, ok := store.(interface {
		DeleteRevocationAndAudit(context.Context, string, string,
			func(identity.AgentRevocation) (controlplane.AuthorizationAuditEvent, error)) (identity.AgentRevocation, error)
	}); ok {
		return func(ctx context.Context, actor, environment, id string) (identity.AgentRevocation, error) {
			return durable.DeleteRevocationAndAudit(ctx, environment, id,
				func(removed identity.AgentRevocation) (controlplane.AuthorizationAuditEvent, error) {
					return agentRevocationAuditEvent(now(), actor, revocationhttp.ActionRemove, &removed, nil)
				})
		}
	}
	if volatile, ok := store.(interface {
		DeleteRevocationAudited(context.Context, string, string,
			func(context.Context, identity.AgentRevocation) error) (identity.AgentRevocation, error)
	}); ok {
		return func(ctx context.Context, actor, environment, id string) (identity.AgentRevocation, error) {
			return volatile.DeleteRevocationAudited(ctx, environment, id,
				func(ctx context.Context, removed identity.AgentRevocation) error {
					return auditAgentRevocation(ctx, repository, now(), actor, revocationhttp.ActionRemove, &removed, nil)
				})
		}
	}
	return nil
}

// auditAgentRevocation records the content of a deny-list change: who made
// it, what was there before and what is there now. Governance has already
// recorded that the operator was allowed to; this is what they then did.
func auditAgentRevocation(
	ctx context.Context, repository controlplane.Repository, now time.Time, subject, action string,
	before, after *identity.AgentRevocation,
) error {
	event, err := agentRevocationAuditEvent(now, subject, action, before, after)
	if err != nil {
		return err
	}
	_, err = repository.AppendAuthorizationAudit(ctx, event)
	return err
}

// agentRevocationAuditEvent constructs the same event for both store kinds.
// PostgreSQL appends it inside the removal's transaction; memory appends it
// while retaining its deny-list lock and deletes only after the append succeeds.
func agentRevocationAuditEvent(
	now time.Time, subject, action string, before, after *identity.AgentRevocation,
) (controlplane.AuthorizationAuditEvent, error) {
	target := after
	if target == nil {
		target = before
	}
	resource := "environment/" + target.Environment
	instant := now.UTC()
	digest := sha256.Sum256([]byte(action + "\x00" + target.ID + "\x00" + subject + "\x00" + instant.Format(time.RFC3339Nano)))
	describe := func(revocation *identity.AgentRevocation) any {
		if revocation == nil {
			return nil
		}
		described := map[string]any{
			"scope": revocation.Scope, "agentId": revocation.AgentID, "serial": revocation.Serial,
			"reason": revocation.Reason, "createdBy": revocation.CreatedBy,
		}
		if !revocation.NotBefore.IsZero() {
			described["notBefore"] = revocation.NotBefore.Format(time.RFC3339Nano)
		}
		return described
	}
	event := controlplane.AuthorizationAuditEvent{
		ID: "aud_" + hex.EncodeToString(digest[:16]), SchemaVersion: controlplane.AuditSchemaV3,
		Categories: []string{controlplane.AuditCategoryConfigureInfra}, TraceID: resource,
		Subject: subject, Action: action, ResourceRID: resource,
		Decision: "ALLOW", Reason: "Agent revocation changed",
		RequestFields: map[string]any{
			"configureInfraTargets": []string{resource},
			"revocationId":          target.ID,
			"revocationBefore":      describe(before),
			"revocationAfter":       describe(after),
		},
		ResultFields: map[string]any{"configureInfraRequestId": target.ID},
		CreatedAt:    instant,
	}
	if err := controlplane.ValidateAuditCategoryEvent(event); err != nil {
		return controlplane.AuthorizationAuditEvent{}, err
	}
	return event, nil
}

func writeAgentCredentialJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
