package composition

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/assistant/adapters/toolmcp"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// auditOriginConsole marks an assistant tool call that did not arrive over
// MCP: the console and anything else calling /v1/assistant directly.
const auditOriginConsole = "console"

// toolOrigin names the surface a tool call arrived on, for its audit entry.
func toolOrigin(ctx context.Context) string {
	return toolmcp.OriginOf(ctx, auditOriginConsole)
}

// mountMCP registers the read-only MCP endpoint on the assistant's mux. The
// tool port is the assistant's own, so a call is validated, authorized as the
// caller and audited exactly as the same call from the console is.
func mountMCP(mux *http.ServeMux, api assistantAPI, service *controlplane.GovernanceService, options controlplane.HandlerOptions) {
	mux.Handle(toolmcp.Route, toolmcp.Handler(toolmcp.Config{
		Tools: api,
		Caller: func(request *http.Request) (application.Actor, error) {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok || strings.TrimSpace(principal.Username) == "" {
				return application.Actor{}, fmt.Errorf("unauthorized")
			}
			return application.Actor{Username: principal.Username, Teams: principal.Teams, Groups: principal.Groups}, nil
		},
	}))
}
