// Package packagingmcp exposes the packaging toolbox as tools over MCP.
package packagingmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const (
	serverName             = "packaging-tools"
	serverVersion          = "1.0.0"
	ToolInspect            = "inspect_image"
	ToolValidate           = "validate_manifests"
	ToolGates              = "dry_run_gates"
	ToolInspectRepo        = "inspect_repo"
	ToolReadFile           = "read_file"
	ToolValidateDockerfile = "validate_dockerfile"
)

// Server is the MCP adapter around a Toolbox.
type Server struct {
	tools domain.Toolbox
	inner *mcp.Server
}

type inspectInput struct {
	Ref string `json:"ref" jsonschema:"image reference or digest to inspect"`
}

type validateInput struct {
	Manifests string `json:"manifests" jsonschema:"Kubernetes YAML to check against packaging conventions"`
}

type gatesInput struct {
	Source          string `json:"source" jsonschema:"product source document"`
	Zarf            string `json:"zarf" jsonschema:"package config file"`
	CR              string `json:"cr" jsonschema:"package custom resource file"`
	SourceDigest    string `json:"sourceDigest,omitempty"`
	TemplateVersion string `json:"templateVersion,omitempty"`
	ToolVersions    string `json:"toolVersions,omitempty"`
	Dockerfile      string `json:"dockerfile,omitempty"`
	Dockerignore    string `json:"dockerignore,omitempty"`
}

type readFileInput struct {
	Path string `json:"path" jsonschema:"path inside the bound repository"`
}

type validateDockerfileInput struct {
	Dockerfile   string `json:"dockerfile" jsonschema:"Dockerfile contents to check"`
	Dockerignore string `json:"dockerignore,omitempty" jsonschema:"dockerignore contents, if any"`
}

// repositoryProbe is implemented by a toolbox that knows whether a source
// repository is bound to it.
type repositoryProbe interface {
	RepositoryBound() bool
}

// RepositoryTools are the tools that read the bound source repository.
func RepositoryTools() []string { return []string{ToolInspectRepo, ToolReadFile} }

// NewServer registers the packaging tools on one MCP server. The two
// repository tools are registered only when the toolbox has a repository
// bound: a tool that could only answer "no repository is bound" is left off
// tools/list rather than offered and failed.
func NewServer(tools domain.Toolbox) *Server {
	inner := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	adapter := &Server{tools: tools, inner: inner}
	mcp.AddTool(inner, &mcp.Tool{Name: ToolInspect, Description: "Read an image config from a registry and return ports, user, entrypoint, command, environment names, and labels."}, adapter.inspect)
	mcp.AddTool(inner, &mcp.Tool{Name: ToolValidate, Description: "Check Kubernetes manifests against packaging conventions."}, adapter.validate)
	mcp.AddTool(inner, &mcp.Tool{Name: ToolGates, Description: "Run the deterministic packaging and Dockerfile gates on a proposed package with no side effects."}, adapter.gates)
	if probe, ok := tools.(repositoryProbe); !ok || probe.RepositoryBound() {
		mcp.AddTool(inner, &mcp.Tool{Name: ToolInspectRepo, Description: "Inspect the bound source repository and return a file tree, detected language, package manager, build scripts, start command, ports, and whether the site is static or a server."}, adapter.inspectRepo)
		mcp.AddTool(inner, &mcp.Tool{Name: ToolReadFile, Description: "Read one size-capped text file from inside the bound source repository."}, adapter.readFile)
	}
	mcp.AddTool(inner, &mcp.Tool{Name: ToolValidateDockerfile, Description: "Check a Dockerfile and dockerignore against packaging containerization conventions."}, adapter.validateDockerfile)
	return adapter
}

// Run serves MCP over the given transport until the client disconnects.
func (server *Server) Run(ctx context.Context, transport mcp.Transport) error {
	return server.inner.Run(ctx, transport)
}

// Connect starts a session on the given transport.
func (server *Server) Connect(ctx context.Context, transport mcp.Transport) (*mcp.ServerSession, error) {
	return server.inner.Connect(ctx, transport, nil)
}

func (server *Server) inspect(ctx context.Context, _ *mcp.CallToolRequest, input inspectInput) (*mcp.CallToolResult, domain.ImageInspect, error) {
	if server.tools == nil {
		return nil, domain.ImageInspect{}, fmt.Errorf("packaging tools are not configured")
	}
	out, err := server.tools.InspectImage(ctx, input.Ref)
	return nil, out, err
}

func (server *Server) validate(ctx context.Context, _ *mcp.CallToolRequest, input validateInput) (*mcp.CallToolResult, domain.ManifestReport, error) {
	if server.tools == nil {
		return nil, domain.ManifestReport{}, fmt.Errorf("packaging tools are not configured")
	}
	out, err := server.tools.ValidateManifests(ctx, input.Manifests)
	return nil, out, err
}

func (server *Server) gates(ctx context.Context, _ *mcp.CallToolRequest, input gatesInput) (*mcp.CallToolResult, domain.Validation, error) {
	if server.tools == nil {
		return nil, domain.Validation{}, fmt.Errorf("packaging tools are not configured")
	}
	out, err := server.tools.DryRunGates(ctx, domain.GateInput{
		Source: input.Source, Zarf: input.Zarf, CR: input.CR,
		SourceDigest: input.SourceDigest, TemplateVersion: input.TemplateVersion, ToolVersions: input.ToolVersions,
		Dockerfile: input.Dockerfile, Dockerignore: input.Dockerignore,
	})
	return nil, out, err
}

func (server *Server) inspectRepo(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, domain.RepoInspect, error) {
	if server.tools == nil {
		return nil, domain.RepoInspect{}, fmt.Errorf("packaging tools are not configured")
	}
	out, err := server.tools.InspectRepo(ctx)
	return nil, out, err
}

func (server *Server) readFile(ctx context.Context, _ *mcp.CallToolRequest, input readFileInput) (*mcp.CallToolResult, domain.RepoFile, error) {
	if server.tools == nil {
		return nil, domain.RepoFile{}, fmt.Errorf("packaging tools are not configured")
	}
	out, err := server.tools.ReadFile(ctx, input.Path)
	return nil, out, err
}

func (server *Server) validateDockerfile(ctx context.Context, _ *mcp.CallToolRequest, input validateDockerfileInput) (*mcp.CallToolResult, domain.ManifestReport, error) {
	if server.tools == nil {
		return nil, domain.ManifestReport{}, fmt.Errorf("packaging tools are not configured")
	}
	out, err := server.tools.ValidateDockerfile(ctx, input.Dockerfile, input.Dockerignore)
	return nil, out, err
}
