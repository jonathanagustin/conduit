package domain

import "context"

// Toolbox is the outbound port for packaging tools a proposer may call.
type Toolbox interface {
	InspectImage(ctx context.Context, ref string) (ImageInspect, error)
	ValidateManifests(ctx context.Context, manifests string) (ManifestReport, error)
	DryRunGates(ctx context.Context, input GateInput) (Validation, error)
	InspectRepo(ctx context.Context) (RepoInspect, error)
	ReadFile(ctx context.Context, path string) (RepoFile, error)
	ValidateDockerfile(ctx context.Context, dockerfile, dockerignore string) (ManifestReport, error)
}

// ImageInspect is the image config a registry returns for a ref or digest.
// Env lists variable names only; values are omitted.
type ImageInspect struct {
	Ref          string            `json:"ref"`
	Digest       string            `json:"digest,omitempty"`
	User         string            `json:"user,omitempty"`
	ExposedPorts []string          `json:"exposedPorts,omitempty"`
	Entrypoint   []string          `json:"entrypoint,omitempty"`
	Cmd          []string          `json:"cmd,omitempty"`
	Env          []string          `json:"env,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// ManifestFinding is one convention or schema check on a manifest.
type ManifestFinding struct {
	Path     string `json:"path"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// ManifestReport is the result of ValidateManifests. OK is false when any
// finding has severity "error".
type ManifestReport struct {
	OK       bool              `json:"ok"`
	Findings []ManifestFinding `json:"findings,omitempty"`
}

// GateInput is a proposed package passed to DryRunGates. Empty digest and
// version fields are filled by the adapter from the source and the current
// template and tool versions.
type GateInput struct {
	Source          string `json:"source"`
	Zarf            string `json:"zarf"`
	CR              string `json:"cr"`
	SourceDigest    string `json:"sourceDigest,omitempty"`
	TemplateVersion string `json:"templateVersion,omitempty"`
	ToolVersions    string `json:"toolVersions,omitempty"`
	Dockerfile      string `json:"dockerfile,omitempty"`
	Dockerignore    string `json:"dockerignore,omitempty"`
}

// RepoInspect is a size-capped view of a bound source repository.
type RepoInspect struct {
	Tree           []string `json:"tree"`
	Language       string   `json:"language,omitempty"`
	PackageManager string   `json:"packageManager,omitempty"`
	BuildScripts   []string `json:"buildScripts,omitempty"`
	StartCommand   string   `json:"startCommand,omitempty"`
	Ports          []int    `json:"ports,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	HasDockerfile  bool     `json:"hasDockerfile"`
}

// RepoFile is a size-capped read of one path inside a bound repository.
type RepoFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SHA256    string `json:"sha256,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}
