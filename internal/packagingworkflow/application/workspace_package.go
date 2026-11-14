package application

import (
	"context"
	"fmt"
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

// WorkspaceStore is a git-backed branch the packager commits into.
type WorkspaceStore interface {
	Prepare(ctx context.Context, dir, repoURL, ref, branch string) (dirOut, branchOut string, err error)
	Commit(ctx context.Context, dir string, files map[string]string, message string) (string, error)
}

// WorkspaceResult is one packaging attempt after gates and a git commit.
type WorkspaceResult struct {
	Branch     string                 `json:"branch"`
	Head       string                 `json:"head,omitempty"`
	Attempts   []WorkspaceAttempt     `json:"attempts"`
	Proposal   domain.PackageProposal `json:"proposal"`
	Provenance domain.Provenance      `json:"provenance"`
	Outcome    string                 `json:"outcome,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
	Archive    string                 `json:"archive,omitempty"`
	Error      string                 `json:"error,omitempty"`
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
	Writer PackageWriter
	Checks PackageChecks
	Store  WorkspaceStore
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
		if logExcerpt != "" {
			input.BuildLog = logExcerpt
			input.Task = domain.TaskRevise
		}
		proposal, provenance, err := packager.Writer.ProposePackage(ctx, input)
		provenance = provenance.WithSkillDigest(input)
		result.Proposal = proposal
		result.Provenance = provenance
		if err != nil {
			cycle.Outcome, cycle.Reason = "error", err.Error()
			result.Attempts = append(result.Attempts, cycle)
			result.Error = err.Error()
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
			return result, nil
		}
		cycle.Head = head
		result.Head = head
		result.Provenance.DockerfileSHA256 = dockerfileDigest(proposal)
		result.Provenance.ResolvedCommit = head
		result.Provenance.RepoURL = repoURL
		reason := packager.gate(ctx, workDir, proposal)
		if reason == "" {
			archive, err := packager.Checks.Create(ctx, workDir, workDir+"/out")
			if err != nil {
				reason = err.Error()
			} else {
				cycle.Outcome = string(domain.OutcomePass)
				cycle.Reason = "lint and render passed"
				result.Attempts = append(result.Attempts, cycle)
				result.Outcome, result.Reason, result.Archive = cycle.Outcome, cycle.Reason, archive
				return result, nil
			}
		}
		cycle.Outcome = string(domain.OutcomeFailRequired)
		cycle.Reason = reason
		cycle.LogExcerpt = reason
		result.Attempts = append(result.Attempts, cycle)
		result.Outcome, result.Reason = cycle.Outcome, reason
		logExcerpt = reason
		if attempt > MaxPackageRepairs {
			return result, nil
		}
	}
	return result, nil
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
	return fmt.Sprintf("workspace package attempt %d: %s", attempt, rationale)
}

func dockerfileDigest(proposal domain.PackageProposal) string {
	if strings.TrimSpace(proposal.Dockerfile) == "" {
		return ""
	}
	return domain.DigestText([]byte(proposal.Dockerfile))
}
