package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
)

const MaxPackageRepairs = 2

// PackageWriter asks a proposer for Dockerfile and package files.
type PackageWriter interface {
	ProposePackage(ctx context.Context, input domain.PackagingContext) (domain.PackageProposal, domain.Provenance, error)
}

// PackageChecks are deterministic package and Dockerfile gates.
type PackageChecks interface {
	Dockerfile(dockerfile, dockerignore string) string
	PackageCR(raw string) string
	Lint(ctx context.Context, dir string) error
	Create(ctx context.Context, dir, outDir string) (string, error)
}

// ImageBuilder is an optional governed image build step that streams build logs.
type ImageBuilder interface {
	BuildImage(ctx context.Context, dir string, logOut io.Writer) (string, error)
}

// ImagePublisher pushes a built tag and returns the repository@sha256 reference
// the registry recorded. The engine image id is not that reference.
type ImagePublisher interface {
	Publish(ctx context.Context, tag string) (string, error)
}

// ImageInspector is an optional OCI image inspector run on the built or packaged image.
type ImageInspector interface {
	InspectImage(ctx context.Context, ref string) (domain.ImageInspect, error)
}

// WorkspaceAuditor records the governed packaging audit event.
type WorkspaceAuditor interface {
	RecordWorkspacePackage(ctx context.Context, audit WorkspaceAudit) error
}

// WorkspaceStore is a git-backed branch the packager commits into.
type WorkspaceStore interface {
	Prepare(ctx context.Context, dir, repoURL, ref, branch string) (dirOut, branchOut string, err error)
	SelectCommit(ctx context.Context, dir, commit string) error
	Commit(ctx context.Context, dir string, files map[string]string, message string) (string, error)
}

// WorkspaceAudit is the governed audit record for a workspace packaging run.
type WorkspaceAudit struct {
	Outcome          string   `json:"outcome"`
	Repo             string   `json:"repo"`
	RepoURL          string   `json:"repoUrl"`
	Commit           string   `json:"commit"`
	ResolvedCommit   string   `json:"resolvedCommit"`
	DockerfileSHA256 string   `json:"dockerfileSha256"`
	ImageDigest      string   `json:"imageDigest"`
	SkillSHA256      string   `json:"skillSha256"`
	ToolCalls        []string `json:"toolCalls"`
	PromptSHA256     string   `json:"promptSha256"`
	ResponseSHA256   string   `json:"responseSha256"`
	// Adapter is the model transport. ConfiguredModel and ProviderModel are
	// the operator-configured name and the provider-reported id; all three
	// are omitted when the writer recorded none.
	Adapter         string `json:"adapter,omitempty"`
	ConfiguredModel string `json:"configuredModel,omitempty"`
	ProviderModel   string `json:"providerModel,omitempty"`
}

// Metadata returns the audit fields as a governance metadata map.
func (audit WorkspaceAudit) Metadata() map[string]any {
	calls := audit.ToolCalls
	if calls == nil {
		calls = []string{}
	}
	metadata := map[string]any{
		"outcome":          audit.Outcome,
		"repo":             audit.Repo,
		"repoUrl":          audit.RepoURL,
		"commit":           audit.Commit,
		"resolvedCommit":   audit.ResolvedCommit,
		"dockerfileSha256": audit.DockerfileSHA256,
		"imageDigest":      audit.ImageDigest,
		"skillSha256":      audit.SkillSHA256,
		"toolCalls":        calls,
		"promptSha256":     audit.PromptSHA256,
		"responseSha256":   audit.ResponseSHA256,
	}
	for key, value := range map[string]string{
		"adapter": audit.Adapter, "configuredModel": audit.ConfiguredModel, "providerModel": audit.ProviderModel,
	} {
		if value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

// WorkspaceResult is one packaging attempt after gates and a git commit.
type WorkspaceResult struct {
	Branch       string                 `json:"branch"`
	Head         string                 `json:"head,omitempty"`
	Attempts     []WorkspaceAttempt     `json:"attempts"`
	Proposal     domain.PackageProposal `json:"proposal"`
	Provenance   domain.Provenance      `json:"provenance"`
	ImageInspect *domain.ImageInspect   `json:"imageInspect,omitempty"`
	Audit        WorkspaceAudit         `json:"audit"`
	Outcome      string                 `json:"outcome,omitempty"`
	Reason       string                 `json:"reason,omitempty"`
	Archive      string                 `json:"archive,omitempty"`
	Error        string                 `json:"error,omitempty"`
}

// WorkspaceAttempt is one write-commit-gate cycle, including repairs.
type WorkspaceAttempt struct {
	Number     int    `json:"number"`
	Head       string `json:"head,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Reason     string `json:"reason,omitempty"`
	LogExcerpt string `json:"logExcerpt,omitempty"`
}

// WorkspacePackager writes package files as commits and runs the gates.
type WorkspacePackager struct {
	Writer    PackageWriter
	Checks    PackageChecks
	Store     WorkspaceStore
	Builder   ImageBuilder
	Inspector ImageInspector
	Auditor   WorkspaceAuditor
	LogWriter io.Writer
}

// WritePackage prepares a git branch, commits the model-written files, and
// repairs gate failures up to MaxPackageRepairs times.
func (packager WorkspacePackager) WritePackage(ctx context.Context, dir, repoURL, ref, branch string, input domain.PackagingContext) (WorkspaceResult, error) {
	result := WorkspaceResult{}
	input = input.WithSkill(packagingskill.Source, packagingskill.SHA256())
	input.Task = domain.TaskPackage
	if packager.Store == nil || packager.Writer == nil || packager.Checks == nil {
		result.Error = domain.ErrModelUnavailable.Error()
		return result, nil
	}
	workDir, workBranch, err := packager.Store.Prepare(ctx, dir, repoURL, ref, branch)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Branch = workBranch
	logExcerpt := strings.TrimSpace(input.BuildLog)
	for attempt := 1; attempt <= MaxPackageRepairs+1; attempt++ {
		cycle := WorkspaceAttempt{Number: attempt}
		result.ImageInspect = nil
		if logExcerpt != "" {
			input.BuildLog = logExcerpt
			input.Task = domain.TaskRevise
		}
		proposal, provenance, err := packager.Writer.ProposePackage(ctx, input)
		provenance = fillProvenanceDigests(provenance.WithSkillDigest(input), input, proposal)
		result.Proposal = proposal
		result.Provenance = provenance
		if err != nil {
			cycle.Outcome, cycle.Reason = "error", err.Error()
			result.Attempts = append(result.Attempts, cycle)
			result.Error = err.Error()
			result.Audit = buildWorkspaceAudit(result.Provenance, result.Outcome)
			return result, nil
		}
		files := map[string]string{"zarf.yaml": proposal.Zarf, "uds-package.yaml": proposal.CR}
		if proposal.Dockerfile != "" {
			files["Dockerfile"] = proposal.Dockerfile
		}
		if proposal.Dockerignore != "" {
			files[".dockerignore"] = proposal.Dockerignore
		}
		if proposal.Source != "" {
			files["package-source.yaml"] = proposal.Source
		}
		head, err := packager.Store.Commit(ctx, workDir, files, commitMessage(attempt, proposal.Rationale))
		if err != nil {
			cycle.Outcome, cycle.Reason = "error", err.Error()
			result.Attempts = append(result.Attempts, cycle)
			result.Error = err.Error()
			result.Audit = buildWorkspaceAudit(result.Provenance, result.Outcome)
			return result, nil
		}
		cycle.Head = head
		result.Head = head
		result.Provenance.DockerfileSHA256 = dockerfileDigest(proposal)
		result.Provenance.ResolvedCommit = head
		result.Provenance.RepoURL = repoURL
		if input.ImageDigest != "" && result.Provenance.ImageDigest == "" {
			result.Provenance.ImageDigest = input.ImageDigest
		}
		packager.streamf("[attempt %d] committed %s on %s\n", attempt, head, workBranch)
		reason := packager.gate(ctx, workDir, proposal)
		if reason == "" {
			builtRef, buildReason := packager.runBuild(ctx, workDir, proposal, input.ImageRef)
			if buildReason != "" {
				reason = buildReason
			} else if inspectErr := packager.inspectBuiltImage(ctx, builtRef, &result); inspectErr != nil {
				reason = inspectErr.Error()
			} else {
				archive, err := packager.Checks.Create(ctx, workDir, workDir+"/out")
				if err != nil {
					reason = err.Error()
				} else {
					packager.streamf("[attempt %d] package archive created: %s\n", attempt, archive)
					cycle.Outcome = string(domain.OutcomePass)
					cycle.Reason = "lint and render passed"
					result.Attempts = append(result.Attempts, cycle)
					result.Outcome, result.Reason, result.Archive = cycle.Outcome, cycle.Reason, archive
					result.Audit = buildWorkspaceAudit(result.Provenance, result.Outcome)
					if packager.Auditor != nil {
						if auditErr := packager.Auditor.RecordWorkspacePackage(ctx, result.Audit); auditErr != nil {
							return result, auditErr
						}
					}
					return result, nil
				}
			}
		}
		packager.streamf("[attempt %d] gate failed: %s\n", attempt, reason)
		cycle.Outcome = string(domain.OutcomeFailRequired)
		cycle.Reason = reason
		cycle.LogExcerpt = reason
		result.Attempts = append(result.Attempts, cycle)
		result.Outcome, result.Reason = cycle.Outcome, reason
		result.Audit = buildWorkspaceAudit(result.Provenance, result.Outcome)
		logExcerpt = reason
		if attempt > MaxPackageRepairs {
			if packager.Auditor != nil {
				if auditErr := packager.Auditor.RecordWorkspacePackage(ctx, result.Audit); auditErr != nil {
					return result, auditErr
				}
			}
			return result, nil
		}
	}
	return result, nil
}

func (packager WorkspacePackager) streamf(format string, args ...any) {
	if packager.LogWriter == nil {
		return
	}
	_, _ = fmt.Fprintf(packager.LogWriter, format, args...)
}

func (packager WorkspacePackager) runBuild(ctx context.Context, dir string, proposal domain.PackageProposal, defaultRef string) (string, string) {
	builder := packager.Builder
	if builder == nil {
		if candidate, ok := packager.Checks.(ImageBuilder); ok {
			builder = candidate
		}
	}
	if builder == nil || strings.TrimSpace(proposal.Dockerfile) == "" {
		return defaultRef, ""
	}
	var buildBuf bytes.Buffer
	out := io.Writer(&buildBuf)
	if packager.LogWriter != nil {
		out = io.MultiWriter(&buildBuf, packager.LogWriter)
	}
	builtRef, err := builder.BuildImage(ctx, dir, out)
	if err != nil {
		excerpt := strings.TrimSpace(buildBuf.String())
		if excerpt == "" {
			excerpt = err.Error()
		}
		return "", excerpt
	}
	if strings.TrimSpace(builtRef) == "" {
		builtRef = defaultRef
	}
	return builtRef, ""
}

func (packager WorkspacePackager) inspectBuiltImage(ctx context.Context, ref string, result *WorkspaceResult) error {
	inspector := packager.Inspector
	if inspector == nil {
		if candidate, ok := packager.Checks.(ImageInspector); ok {
			inspector = candidate
		}
	}
	if inspector == nil {
		return nil
	}
	// Once inspection is configured, proposed/input digests cannot stand in for
	// facts about a built reference whose inspection has not succeeded.
	result.Provenance.ImageDigest = ""
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("image reference is required for configured inspection")
	}
	inspected, err := inspector.InspectImage(ctx, ref)
	if err != nil {
		return fmt.Errorf("inspect image: %w", err)
	}
	if strings.TrimSpace(inspected.Digest) == "" {
		return fmt.Errorf("image inspection returned no digest")
	}
	result.ImageInspect = &inspected
	result.Provenance.ImageDigest = strings.TrimSpace(inspected.Digest)
	result.Provenance.ToolCalls = appendToolCallOnce(result.Provenance.ToolCalls, "inspect_image")
	return nil
}

func appendToolCallOnce(calls []domain.ToolCallRecord, name string) []domain.ToolCallRecord {
	for _, call := range calls {
		if call.Name == name {
			return calls
		}
	}
	return append(calls, domain.ToolCallRecord{Name: name})
}

func fillProvenanceDigests(provenance domain.Provenance, input domain.PackagingContext, proposal domain.PackageProposal) domain.Provenance {
	if strings.TrimSpace(provenance.PromptSHA256) == "" {
		provenance.PromptSHA256 = domain.DigestText([]byte(domain.Prompt(input)))
	}
	if strings.TrimSpace(provenance.ResponseSHA256) == "" {
		if raw, err := json.Marshal(proposal); err == nil {
			provenance.ResponseSHA256 = domain.DigestText(raw)
		}
	}
	return provenance
}

func buildWorkspaceAudit(provenance domain.Provenance, outcome string) WorkspaceAudit {
	calls := make([]string, 0, len(provenance.ToolCalls))
	for _, call := range provenance.ToolCalls {
		if strings.TrimSpace(call.Name) != "" {
			calls = append(calls, strings.TrimSpace(call.Name))
		}
	}
	return WorkspaceAudit{
		Outcome:          outcome,
		Repo:             provenance.RepoURL,
		RepoURL:          provenance.RepoURL,
		Commit:           provenance.ResolvedCommit,
		ResolvedCommit:   provenance.ResolvedCommit,
		DockerfileSHA256: provenance.DockerfileSHA256,
		ImageDigest:      provenance.ImageDigest,
		SkillSHA256:      provenance.SkillSHA256,
		ToolCalls:        calls,
		PromptSHA256:     provenance.PromptSHA256,
		ResponseSHA256:   provenance.ResponseSHA256,
		Adapter:          provenance.Adapter,
		ConfiguredModel:  provenance.ConfiguredModel,
		ProviderModel:    provenance.ProviderModel,
	}
}

func (packager WorkspacePackager) gate(ctx context.Context, dir string, proposal domain.PackageProposal) string {
	if proposal.Dockerfile != "" {
		if reason := packager.Checks.Dockerfile(proposal.Dockerfile, proposal.Dockerignore); reason != "" {
			return reason
		}
	}
	if reason := packager.Checks.PackageCR(proposal.CR); reason != "" {
		return reason
	}
	if err := packager.Checks.Lint(ctx, dir); err != nil {
		return err.Error()
	}
	return ""
}

func commitMessage(attempt int, rationale string) string {
	rationale = strings.TrimSpace(rationale)
	if rationale == "" {
		rationale = "write package files"
	}
	return fmt.Sprintf("chore(workspace): package attempt %d: %s", attempt, rationale)
}

func dockerfileDigest(proposal domain.PackageProposal) string {
	if strings.TrimSpace(proposal.Dockerfile) == "" {
		return ""
	}
	return domain.DigestText([]byte(proposal.Dockerfile))
}
