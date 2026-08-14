// Package rubixsubstrate carries the recovered Rubix networking and credential
// control surface as a typed, validated route table.
//
// Provenance: the fourteen endpoints below were read out of the Conjure-generated
// Dialogue endpoint enums -- each constant carries the path template, verb,
// service and endpoint name -- in
//
//	com.palantir.rubixlib:network-api:2.512.0            (egress/ingress allowlist, metadata)
//	com.palantir.rubixlib:user-credential-manager:2.512.0 (provisioning, validation)
//
// fetched from the tenant's artifacts API on 2026-08-13. This is Palantir's own
// request surface, transcribed, not authored. Only the operation identity
// (service, endpoint, method, path) was recovered; the request and response wire
// beans were not, so -- exactly as internal/computecontrol does for the Flink
// control plane -- this package defines no wire DTOs and no fabricated port
// signatures. A future adapter that gains the recovered objects jar owns those.
//
// The canonical table here is the authoritative in-code copy of the recovered
// contract. routes_test.go binds it to deploy/rubix-substrate/http-contract.json
// so the code and the committed ground-truth fixture cannot drift apart.
package rubixsubstrate

import (
	"fmt"
	"sort"
	"strings"
)

// Service is a recovered Rubix Conjure service name.
type Service string

const (
	EgressAllowlistConfigurationService  Service = "EgressAllowlistConfigurationService"
	IngressAllowlistConfigurationService Service = "IngressAllowlistConfigurationService"
	MetadataRetrievalService             Service = "MetadataRetrievalService"
	UserCredentialManagerProvisioning    Service = "UserCredentialManagerProvisioningService"
	UserCredentialManagerValidation      Service = "UserCredentialManagerValidationService"
)

// Route is one recovered operation: its service, endpoint name, HTTP verb and
// path template. Path parameters appear as {name} segments, verbatim from the
// recovered enum.
type Route struct {
	Service  Service
	Endpoint string
	Method   string
	Path     string
}

// Routes is the canonical recovered contract: all fourteen operations across the
// five Rubix networking and credential services, in the recovered order.
var Routes = []Route{
	{EgressAllowlistConfigurationService, "setEgressConfig", "PUT", "/egress-allowlist"},
	{EgressAllowlistConfigurationService, "getEgressConfig", "GET", "/egress-allowlist"},
	{EgressAllowlistConfigurationService, "checkEgressEnabled", "GET", "/egress-allowlist/enabled"},
	{IngressAllowlistConfigurationService, "setDomainIngress", "PUT", "/ingress-allowlist"},
	{IngressAllowlistConfigurationService, "listDomainIngresses", "GET", "/ingress-allowlist"},
	{IngressAllowlistConfigurationService, "checkIngressEnabled", "GET", "/ingress-allowlist/enabled"},
	{IngressAllowlistConfigurationService, "getFallbackConfig", "GET", "/ingress-allowlist/fallback"},
	{IngressAllowlistConfigurationService, "revertIngressToFallbackConfig", "POST", "/ingress-allowlist/rollback/{domain}"},
	{MetadataRetrievalService, "getEgressCidrs", "GET", "/metadata/egress"},
	{UserCredentialManagerProvisioning, "retrieveCredential", "POST", "/provision/{username}"},
	{UserCredentialManagerProvisioning, "getClusterConfigurations", "POST", "/provision/{username}/clusters"},
	{UserCredentialManagerProvisioning, "RetrieveCredentialForInfraUser", "POST", "/provision/{username}/infra-user"},
	{UserCredentialManagerProvisioning, "retrieveServiceAccountCredentialFromToken", "GET", "/provision/token"},
	{UserCredentialManagerValidation, "validateConfig", "POST", "/validate/{username}"},
}

// RouteFor returns the recovered route for a service and endpoint. The typed
// client resolves every call through this, so it cannot address a path or verb
// the gated table does not carry.
func RouteFor(service Service, endpoint string) (Route, bool) {
	for _, route := range Routes {
		if route.Service == service && route.Endpoint == endpoint {
			return route, true
		}
	}
	return Route{}, false
}

var validMethods = map[string]bool{"GET": true, "PUT": true, "POST": true, "DELETE": true, "PATCH": true}

// Validate proves the table's internal invariants: every route is well-formed,
// no (service, endpoint) pair repeats, and no (method, path) pair repeats. It
// does not reach outside the package; the fixture cross-check lives in the test.
func Validate() error {
	seenOp := map[string]bool{}
	seenRoute := map[string]bool{}
	for _, route := range Routes {
		if route.Service == "" || route.Endpoint == "" {
			return fmt.Errorf("route %+v has an empty service or endpoint", route)
		}
		if !validMethods[route.Method] {
			return fmt.Errorf("%s.%s has an unrecognized method %q", route.Service, route.Endpoint, route.Method)
		}
		if !strings.HasPrefix(route.Path, "/") {
			return fmt.Errorf("%s.%s path %q is not rooted", route.Service, route.Endpoint, route.Path)
		}
		if err := validatePathParameters(route.Path); err != nil {
			return fmt.Errorf("%s.%s: %w", route.Service, route.Endpoint, err)
		}
		op := string(route.Service) + "." + route.Endpoint
		if seenOp[op] {
			return fmt.Errorf("duplicate operation %s", op)
		}
		seenOp[op] = true
		wire := route.Method + " " + route.Path
		if seenRoute[wire] {
			return fmt.Errorf("duplicate route %s", wire)
		}
		seenRoute[wire] = true
	}
	return nil
}

// validatePathParameters checks each {name} segment is a whole, non-empty
// segment with no stray braces.
func validatePathParameters(path string) error {
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		open := strings.Count(segment, "{")
		closed := strings.Count(segment, "}")
		if open != closed {
			return fmt.Errorf("path %q has an unbalanced parameter segment %q", path, segment)
		}
		if open == 0 {
			continue
		}
		if open != 1 || !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			return fmt.Errorf("path %q parameter segment %q is not a whole {name}", path, segment)
		}
		if len(segment) <= 2 {
			return fmt.Errorf("path %q has an empty parameter name", path)
		}
	}
	return nil
}

// ServicesInOrder returns the distinct services in first-seen order, so callers
// can enumerate the recovered surface without re-deriving it.
func ServicesInOrder() []Service {
	seen := map[Service]bool{}
	var order []Service
	for _, route := range Routes {
		if !seen[route.Service] {
			seen[route.Service] = true
			order = append(order, route.Service)
		}
	}
	return order
}

// key is the stable identity of a route, used to compare against the fixture.
func (route Route) key() string {
	return string(route.Service) + "|" + route.Endpoint + "|" + route.Method + "|" + route.Path
}

// SortedKeys returns every route's stable key, sorted, for set comparison.
func SortedKeys() []string {
	keys := make([]string, 0, len(Routes))
	for _, route := range Routes {
		keys = append(keys, route.key())
	}
	sort.Strings(keys)
	return keys
}
