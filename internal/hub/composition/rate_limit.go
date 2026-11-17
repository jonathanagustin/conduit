package composition

import (
	"encoding/json"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/ratelimit"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
)

// RateLimit puts a per-caller token bucket in front of an assembled Hub.
//
// It wraps the whole surface, outside guardUnregisteredRoutes, so that nothing
// the Hub serves or refuses is reachable without spending a token. It mounts
// no route of its own.
//
// Who is asking is decided the same way the handlers decide it, and before the
// bucket is chosen:
//
//   - A route HubRouteAuthorization registers as DecisionAgent, called with an
//     Agent identity, spends from that Agent's bucket in the Agent class.
//     Agent buckets are a class of their own: no amount of operator traffic
//     can empty one, because the two classes share no state.
//   - Any other registered route, called with operator claims that resolve,
//     spends from that operator's bucket. The key is the authenticated
//     username, not a delegated on-behalf-of name.
//   - Everything else -- no credential, a credential that does not resolve, a
//     path the Hub does not serve -- spends from a stricter bucket keyed by the
//     connection's remote address.
//
// The remote address is the TCP peer. X-Forwarded-For and Forwarded are not
// read: any client can write them, so honouring them would let a caller pick a
// fresh bucket per request. The consequence is stated rather than hidden: a
// Hub behind a gateway sees every unauthenticated request arrive from the
// gateway's address, so those requests share one anonymous bucket. Size the
// anonymous limit for that, or limit anonymous traffic at the gateway, which
// does know the client.
//
// Liveness and readiness are exempt. A probe that was refused would restart a
// Hub for being busy.
//
// A request is counted once, on arrival. A long-lived response -- the
// live-event stream -- costs one token to open and is never throttled while
// it is open.
func RateLimit(hub http.Handler, options controlplane.HandlerOptions, limiter *ratelimit.Limiter) http.Handler {
	if limiter == nil {
		return hub
	}
	routes := http.NewServeMux()
	for _, route := range HubRouteAuthorization() {
		routes.Handle(route.Pattern, routeDecision(route.Decision))
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		class, key, exempt := rateLimitSubject(routes, request, options)
		// The access log reports the class decided here; see ObserveRequests.
		noteCallerClass(request, class)
		if exempt {
			hub.ServeHTTP(writer, request)
			return
		}
		decision := limiter.Allow(class, key)
		if decision.Allowed {
			hub.ServeHTTP(writer, request)
			return
		}
		// Whole seconds, rounded up: a client told to wait 0 would retry at once.
		seconds := int(math.Ceil(decision.RetryAfter.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		writer.Header().Set("Retry-After", strconv.Itoa(seconds))
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(writer).Encode(map[string]string{
			"error": "rate limit exceeded; retry after " + (time.Duration(seconds) * time.Second).String(), "status": "429",
		})
	})
}

// routeDecision carries a route's registered decision through a ServeMux, so
// the limiter classifies a request by the same register the guard routes by.
type routeDecision Decision

func (routeDecision) ServeHTTP(http.ResponseWriter, *http.Request) {}

func rateLimitSubject(
	routes *http.ServeMux, request *http.Request, options controlplane.HandlerOptions,
) (class ratelimit.Class, key string, exempt bool) {
	matched, pattern := routes.Handler(request)
	decision, registered := matched.(routeDecision)
	switch {
	case !registered || pattern == "":
		// Not a route this Hub has. No credential is examined for a 404.
	case pattern == "GET /health/live" || pattern == "GET /health/ready":
		return "", "", true
	case Decision(decision) == DecisionAgent:
		if agent, err := httpidentity.AgentIdentityFromRequest(request, options.AllowInsecureAgentIdentityHeaders); err == nil {
			return ratelimit.ClassAgent, agent.Environment + "/" + agent.AgentID, false
		}
	case Decision(decision) == DecisionPublic:
		// Served to anybody; nobody's identity is consulted.
	default:
		if claims, err := operatorClaims(request, options); err == nil && strings.TrimSpace(claims.Username) != "" {
			key := "user:"
			if claims.ServiceAccount {
				key = "service-account:"
			}
			return ratelimit.ClassOperator, key + claims.Username, false
		}
	}
	return ratelimit.ClassAnonymous, remoteAddressKey(request.RemoteAddr), false
}

// remoteAddressKey names the TCP peer. An IPv6 peer is keyed by its /64,
// because one subscriber is routinely delegated a whole /64 and could
// otherwise present a new address, and so a new bucket, for every request.
func remoteAddressKey(remote string) string {
	address, err := netip.ParseAddrPort(remote)
	if err != nil {
		parsed, parseErr := netip.ParseAddr(remote)
		if parseErr != nil {
			return "unknown"
		}
		address = netip.AddrPortFrom(parsed, 0)
	}
	peer := address.Addr().Unmap().WithZone("")
	if peer.Is6() {
		if prefix, err := peer.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return peer.String()
}
