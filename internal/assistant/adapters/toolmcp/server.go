// Package toolmcp serves the assistant's read tools over the Model Context
// Protocol, on the Streamable HTTP transport. It is an inbound adapter: every
// call goes through the same tool port the console uses, so argument
// validation, authorization as the caller and the per-call audit entry are
// the assistant's own. It offers no tool that writes.
package toolmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

const (
	// Route is the one MCP endpoint. Streamable HTTP in stateless mode uses
	// POST only; the SDK answers GET and DELETE with 405.
	Route = "POST /v1/mcp"

	// ProtocolVersion is the one MCP protocol revision this endpoint speaks.
	// It is pinned rather than left to the SDK's default so an SDK upgrade
	// cannot change the wire contract unnoticed.
	ProtocolVersion = "2025-11-25"

	// ServerName and ServerVersion are reported in the initialize result.
	ServerName    = "conduit-hub"
	ServerVersion = "1.0.0"

	// Mode is the assistant mode every MCP call runs in. It is the read-only
	// mode with the most tools; the tool port refuses anything outside it.
	Mode = domain.ModeConfigDiff

	// MaxRequestBytes bounds one HTTP request body.
	MaxRequestBytes = 64 << 10
	// MaxResultBytes bounds one tool result. It is the bound the assistant
	// applies to a result before a model sees it.
	MaxResultBytes = domain.MaxToolResultBytes

	// Origin is the audit origin marker for a call that arrived over MCP.
	Origin = "mcp"
)

// Invoker is the assistant tool port.
type Invoker interface {
	Invoke(ctx context.Context, actor application.Actor, mode, name string, arguments json.RawMessage) (application.Result, error)
}

// Config wires the endpoint.
type Config struct {
	Tools Invoker
	// Caller authenticates the request as an operator. An error is a 401.
	Caller func(request *http.Request) (application.Actor, error)
}

type originKey struct{}

// WithOrigin marks a context as serving an MCP call.
func WithOrigin(ctx context.Context) context.Context {
	return context.WithValue(ctx, originKey{}, Origin)
}

// OriginOf returns Origin for a context this adapter marked, and fallback
// for any other.
func OriginOf(ctx context.Context, fallback string) string {
	if origin, ok := ctx.Value(originKey{}).(string); ok && origin != "" {
		return origin
	}
	return fallback
}

// definition is one tool as MCP lists it. The argument shapes are the
// assistant's QueryArgs and DiffArgs; domain.ParseToolCall remains the
// validator, and the schema here only describes it to a client.
type definition struct {
	name, title, description string
	schema                   json.RawMessage
}

var definitions = []definition{
	{
		name:        domain.ToolQuery,
		title:       "Query ontology objects",
		description: "Read objects of one type from the Hub's ontology catalog, limited to what the authenticated operator may read. Returns at most 20 objects and the total match count.",
		schema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["objectType"],"properties":{` +
			`"objectType":{"type":"string","description":"Object type name, for example Environment."},` +
			`"filter":{"type":"object","additionalProperties":false,"required":["field","value"],"properties":{"field":{"type":"string"},"value":{"type":"string"}},"description":"One field equality."},` +
			`"links":{"type":"array","maxItems":4,"items":{"type":"string"},"description":"Link names to follow from the matched objects, in order."}}}`),
	},
	{
		name:        domain.ToolDiff,
		title:       "Review a change request diff",
		description: "Read the stored before/after diff of one change request by id.",
		schema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["changeRequestId"],"properties":{` +
			`"changeRequestId":{"type":"string","description":"Change request id."}}}`),
	},
}

// Tools lists the tool names this endpoint serves: the defined read tools
// that the read-only assistant mode allows. A tool the mode does not allow is
// never listed, and the tool port refuses it again when called.
func Tools() []string {
	var names []string
	for _, tool := range definitions {
		if domain.ModeAllows(Mode, tool.name) {
			names = append(names, tool.name)
		}
	}
	return names
}

// Handler serves MCP over Streamable HTTP. Each POST is authenticated on its
// own: the transport is stateless, so there is no session that could outlive
// or be detached from the credential that opened it.
//
// CONDUIT-INVENTED: assistant.MCPReadToolSurface
// Displaced-by: a published or sanitized Apollo MCP or agent-tool contract defining tools, transport and authority.
func Handler(config Config) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		actor, ok := request.Context().Value(actorKey{}).(application.Actor)
		if !ok {
			return nil
		}
		return newServer(config.Tools, actor)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: MaxRequestBytes})
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if config.Caller == nil || config.Tools == nil {
			writeError(writer, http.StatusServiceUnavailable, "the MCP endpoint is not configured")
			return
		}
		actor, err := config.Caller(request)
		if err != nil || strings.TrimSpace(actor.Username) == "" {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="conduit-hub"`)
			writeError(writer, http.StatusUnauthorized, "an authenticated operator is required")
			return
		}
		ctx := context.WithValue(WithOrigin(request.Context()), actorKey{}, actor)
		streamable.ServeHTTP(writer, request.WithContext(ctx))
	})
}

type actorKey struct{}

func writeError(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": message})
}

func newServer(tools Invoker, actor application.Actor) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: ServerVersion}, &mcp.ServerOptions{
		SupportedProtocolVersions: []string{ProtocolVersion},
	})
	served := map[string]bool{}
	for _, name := range Tools() {
		served[name] = true
	}
	for _, tool := range definitions {
		if !served[tool.name] {
			continue
		}
		name := tool.name
		server.AddTool(&mcp.Tool{
			Name: name, Title: tool.title, Description: tool.description, InputSchema: tool.schema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return call(WithOrigin(ctx), tools, actor, name, request.Params.Arguments), nil
		})
	}
	return server
}

// payload is what a tool call returns: the fields of the assistant's result
// that a read produces, and nothing addressed to the reader.
type payload struct {
	Tool                string                  `json:"tool"`
	OK                  bool                    `json:"ok"`
	Error               string                  `json:"error,omitempty"`
	MatchedCount        int                     `json:"matchedCount,omitempty"`
	Entities            []application.ObjectHit `json:"entities,omitempty"`
	ChangeRequestID     string                  `json:"changeRequestId,omitempty"`
	ChangeRequestStatus string                  `json:"changeRequestStatus,omitempty"`
	Title               string                  `json:"title,omitempty"`
	Hunks               []application.Hunk      `json:"hunks,omitempty"`
	AuditEventID        string                  `json:"auditEventId,omitempty"`
	Truncated           bool                    `json:"truncated,omitempty"`
	Bytes               int                     `json:"bytes,omitempty"`
}

func call(ctx context.Context, tools Invoker, actor application.Actor, name string, arguments json.RawMessage) *mcp.CallToolResult {
	result, err := tools.Invoke(ctx, actor, Mode, name, arguments)
	out := payload{
		Tool: name, OK: result.OK, Error: result.Error, MatchedCount: result.MatchedCount, Entities: result.Entities,
		ChangeRequestID: result.ChangeRequestID, ChangeRequestStatus: result.ChangeRequestStatus,
		Title: result.Title, Hunks: result.Hunks, AuditEventID: result.AuditEventID,
	}
	if err != nil {
		out.OK = false
		out.Error = publicError(err)
	}
	encoded, encodeErr := json.Marshal(out)
	if encodeErr != nil {
		out = payload{Tool: name, Error: "the result could not be encoded", AuditEventID: result.AuditEventID}
		encoded, _ = json.Marshal(out)
	}
	if len(encoded) > MaxResultBytes {
		// Replaced, never cut mid-document: the same rule the assistant
		// applies before a model sees a result.
		out = payload{Tool: name, OK: out.OK, Error: out.Error, MatchedCount: out.MatchedCount, AuditEventID: out.AuditEventID, Truncated: true, Bytes: len(encoded)}
		if len(out.Error) > 512 {
			out.Error = out.Error[:512]
		}
		encoded, _ = json.Marshal(out)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(encoded)}},
		StructuredContent: json.RawMessage(encoded),
		IsError:           !out.OK,
	}
}

// publicError keeps a validation message, which the caller needs, and
// replaces anything else: a store or audit fault is not the caller's to read.
func publicError(err error) string {
	switch {
	case errors.Is(err, domain.ErrToolInvalid):
		return err.Error()
	case errors.Is(err, domain.ErrAuditUnavailable):
		return "the call was refused because its audit entry could not be written"
	default:
		return "the tool failed"
	}
}
