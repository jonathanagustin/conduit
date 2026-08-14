package rubixsubstrate

// The server side of the recovered Rubix networking and credential contract.
//
// This is the faithful mirror of client.go: the same fourteen recovered
// operations, the same recovered request/response beans, dispatched strictly
// through the gated Routes table. A backend implements the Rubix interface --
// one method per recovered endpoint, with the exact recovered bean signatures --
// and NewHandler wires it onto an http.ServeMux keyed by the recovered verb and
// path, so the served surface cannot address an operation the table does not
// carry (the symmetric guarantee to the client resolving through RouteFor).
//
// Only the mechanical Conjure transport is added here: read the recovered JSON
// request bean, invoke the typed method, write the recovered JSON response bean.
// No route, bean, verb, path, or auth policy is invented; the contract is the
// one recovered in routes.go and beans.go. server_test.go stands the real
// Client up against a stub backend and round-trips all fourteen, and asserts the
// handler registers exactly the recovered table.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Rubix is the recovered service surface a backend implements to serve the
// contract. Each method mirrors the correspondingly named Client method: same
// recovered request and response beans, same path parameters. Implementing this
// interface is the whole obligation of a server; the transport is NewHandler's.
type Rubix interface {
	// EgressAllowlistConfigurationService
	SetEgressConfig(ctx context.Context, req ApplyNetworkEgressConfigurationRequest) (ApplyNetworkEgressConfigurationResponse, error)
	GetEgressConfig(ctx context.Context) (EgressConfigurations, error)
	CheckEgressEnabled(ctx context.Context) (bool, error)

	// IngressAllowlistConfigurationService
	SetDomainIngress(ctx context.Context, req SetDomainIngressRequest) (SetDomainIngressResponse, error)
	ListDomainIngresses(ctx context.Context) (IngressConfigurations, error)
	CheckIngressEnabled(ctx context.Context) (bool, error)
	GetFallbackConfig(ctx context.Context) (GetFallbackConfigResponse, error)
	RevertIngressToFallbackConfig(ctx context.Context, domain Domain) (ApplyNetworkIngressRollbackResponse, error)

	// MetadataRetrievalService
	GetEgressCidrs(ctx context.Context) (GetEgressCidrsResponse, error)

	// UserCredentialManagerProvisioningService
	RetrieveCredential(ctx context.Context, username Username, req RetrieveCredentialRequest) (RetrieveCredentialPayload, error)
	GetClusterConfigurations(ctx context.Context, username Username, req RetrieveClusterConfigurationsRequest) (ClusterConfigurations, error)
	RetrieveCredentialForInfraUser(ctx context.Context, username Username, req RetrieveCredentialForInfraUserRequest) (RetrieveCredentialForInfraUserPayload, error)
	RetrieveServiceAccountCredentialFromToken(ctx context.Context) (RetrieveServiceAccountCredentialFromTokenPayload, error)

	// UserCredentialManagerValidationService
	ValidateConfig(ctx context.Context, username Username, req ValidateConfigRequest) (ValidationPayload, error)
}

// NewHandler builds an http.Handler that serves svc over the recovered contract.
// It registers one route per entry in the gated Routes table, using the enhanced
// ServeMux pattern "METHOD /path/{param}" so the recovered path parameters bind
// by name. A route without a matching handler is a wiring error and panics at
// construction, mirroring the client's total resolution through RouteFor.
func NewHandler(svc Rubix) http.Handler {
	dispatch := handlersFor(svc)
	mux := http.NewServeMux()
	for _, route := range Routes {
		handler, ok := dispatch[opKey(route.Service, route.Endpoint)]
		if !ok {
			panic(fmt.Sprintf("rubixsubstrate: no server handler for recovered route %s.%s", route.Service, route.Endpoint))
		}
		mux.HandleFunc(route.Method+" "+route.Path, handler)
	}
	return mux
}

// opKey is the stable (service, endpoint) identity used to bind a handler to its
// recovered route.
func opKey(service Service, endpoint string) string {
	return string(service) + "|" + endpoint
}

// handlersFor returns the typed transport glue for every recovered endpoint,
// keyed by opKey. Each closure decodes the recovered request bean (when the
// endpoint carries one), invokes the typed method with any recovered path
// parameter, and encodes the recovered response bean -- the exact inverse of the
// matching Client method.
func handlersFor(svc Rubix) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		// --- EgressAllowlistConfigurationService ---
		opKey(EgressAllowlistConfigurationService, "setEgressConfig"): func(w http.ResponseWriter, r *http.Request) {
			var req ApplyNetworkEgressConfigurationRequest
			if !decodeBody(w, r, &req) {
				return
			}
			resp, err := svc.SetEgressConfig(r.Context(), req)
			respond(w, resp, err)
		},
		opKey(EgressAllowlistConfigurationService, "getEgressConfig"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.GetEgressConfig(r.Context())
			respond(w, resp, err)
		},
		opKey(EgressAllowlistConfigurationService, "checkEgressEnabled"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.CheckEgressEnabled(r.Context())
			respond(w, resp, err)
		},

		// --- IngressAllowlistConfigurationService ---
		opKey(IngressAllowlistConfigurationService, "setDomainIngress"): func(w http.ResponseWriter, r *http.Request) {
			var req SetDomainIngressRequest
			if !decodeBody(w, r, &req) {
				return
			}
			resp, err := svc.SetDomainIngress(r.Context(), req)
			respond(w, resp, err)
		},
		opKey(IngressAllowlistConfigurationService, "listDomainIngresses"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.ListDomainIngresses(r.Context())
			respond(w, resp, err)
		},
		opKey(IngressAllowlistConfigurationService, "checkIngressEnabled"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.CheckIngressEnabled(r.Context())
			respond(w, resp, err)
		},
		opKey(IngressAllowlistConfigurationService, "getFallbackConfig"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.GetFallbackConfig(r.Context())
			respond(w, resp, err)
		},
		opKey(IngressAllowlistConfigurationService, "revertIngressToFallbackConfig"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.RevertIngressToFallbackConfig(r.Context(), Domain(r.PathValue("domain")))
			respond(w, resp, err)
		},

		// --- MetadataRetrievalService ---
		opKey(MetadataRetrievalService, "getEgressCidrs"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.GetEgressCidrs(r.Context())
			respond(w, resp, err)
		},

		// --- UserCredentialManagerProvisioningService ---
		opKey(UserCredentialManagerProvisioning, "retrieveCredential"): func(w http.ResponseWriter, r *http.Request) {
			var req RetrieveCredentialRequest
			if !decodeBody(w, r, &req) {
				return
			}
			resp, err := svc.RetrieveCredential(r.Context(), Username(r.PathValue("username")), req)
			respond(w, resp, err)
		},
		opKey(UserCredentialManagerProvisioning, "getClusterConfigurations"): func(w http.ResponseWriter, r *http.Request) {
			var req RetrieveClusterConfigurationsRequest
			if !decodeBody(w, r, &req) {
				return
			}
			resp, err := svc.GetClusterConfigurations(r.Context(), Username(r.PathValue("username")), req)
			respond(w, resp, err)
		},
		opKey(UserCredentialManagerProvisioning, "RetrieveCredentialForInfraUser"): func(w http.ResponseWriter, r *http.Request) {
			var req RetrieveCredentialForInfraUserRequest
			if !decodeBody(w, r, &req) {
				return
			}
			resp, err := svc.RetrieveCredentialForInfraUser(r.Context(), Username(r.PathValue("username")), req)
			respond(w, resp, err)
		},
		opKey(UserCredentialManagerProvisioning, "retrieveServiceAccountCredentialFromToken"): func(w http.ResponseWriter, r *http.Request) {
			resp, err := svc.RetrieveServiceAccountCredentialFromToken(r.Context())
			respond(w, resp, err)
		},

		// --- UserCredentialManagerValidationService ---
		opKey(UserCredentialManagerValidation, "validateConfig"): func(w http.ResponseWriter, r *http.Request) {
			var req ValidateConfigRequest
			if !decodeBody(w, r, &req) {
				return
			}
			resp, err := svc.ValidateConfig(r.Context(), Username(r.PathValue("username")), req)
			respond(w, resp, err)
		},
	}
}

// decodeBody reads the recovered JSON request bean into out. A malformed body is
// a 400 and reports false so the caller stops; success reports true.
func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		http.Error(w, fmt.Sprintf("rubixsubstrate: malformed request body: %v", err), http.StatusBadRequest)
		return false
	}
	return true
}

// respond writes the recovered response bean as JSON, or maps a backend error to
// 500 -- the status the Client surfaces as a non-2xx error. It is generic so a
// bool, slice, map, or object response is written identically.
func respond[T any](w http.ResponseWriter, payload T, err error) {
	if err != nil {
		http.Error(w, fmt.Sprintf("rubixsubstrate: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if encodeErr := json.NewEncoder(w).Encode(payload); encodeErr != nil {
		http.Error(w, fmt.Sprintf("rubixsubstrate: encode response: %v", encodeErr), http.StatusInternalServerError)
	}
}
