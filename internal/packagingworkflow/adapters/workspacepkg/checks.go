// Package workspacepkg runs the Dockerfile, Package CR and zarf checks on
// package files committed in a planning workspace.
package workspacepkg

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacegit"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
)

// Checks runs Dockerfile, Package CR, and zarf gates.
type Checks struct {
	Tools *packagingtools.Service
	Zarf  packagingtools.ZarfRunner
}

func (checks Checks) Dockerfile(dockerfile, dockerignore string) string {
	report, err := packagingtools.New().ValidateDockerfile(context.Background(), dockerfile, dockerignore)
	if err != nil {
		return err.Error()
	}
	if report.OK {
		return ""
	}
	for _, finding := range report.Findings {
		if finding.Severity == "error" {
			return finding.Message
		}
	}
	return "dockerfile failed validation"
}

func (checks Checks) PackageCR(raw string) string {
	report := packagingtools.New().ValidatePackageCR(raw)
	if report.OK {
		return ""
	}
	for _, finding := range report.Findings {
		if finding.Severity == "error" {
			return finding.Message
		}
	}
	return "package cr failed validation"
}

func (checks Checks) Lint(ctx context.Context, dir string) error {
	return checks.Zarf.Lint(ctx, dir)
}

func (checks Checks) Create(ctx context.Context, dir, outDir string) (string, error) {
	return checks.Zarf.Create(ctx, dir, outDir)
}

// Store is a git-backed workspace.
type Store struct{}

func (Store) Prepare(ctx context.Context, dir, repoURL, ref, branch string) (string, string, error) {
	snap, err := workspacegit.Prepare(ctx, dir, repoURL, ref, branch)
	if err != nil {
		return "", "", err
	}
	return snap.Dir, snap.Branch, nil
}

func (Store) Commit(ctx context.Context, dir string, files map[string]string, message string) (string, error) {
	return (workspacegit.Repo{Dir: dir}).Commit(ctx, files, message)
}

var _ application.PackageChecks = Checks{}
var _ application.WorkspaceStore = Store{}
