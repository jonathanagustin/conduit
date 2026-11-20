package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
)

// GovernedRequest is one accepted source packaged from the workspace tree.
type GovernedRequest struct {
	Dir       string
	RepoURL   string
	Ref       string
	Commit    string
	Branch    string
	AppName   string
	ImageTag  string
	Runtime   string
	BaseImage string
	Network   string
}

// Governed clones the accepted source, commits model-written package files,
// builds the image, and runs the package gates. Success requires the engine's
// image id and a package archive. A missing tool is a failure.
func (packager WorkspacePackager) Governed(ctx context.Context, req GovernedRequest) (WorkspaceResult, error) {
	result := WorkspaceResult{}
	if packager.Writer == nil || packager.Checks == nil || packager.Builder == nil || packager.Inspector == nil || packager.Store == nil {
		result.Error = domain.ErrModelUnavailable.Error()
		return result, nil
	}
	if strings.TrimSpace(req.ImageTag) == "" || strings.TrimSpace(req.AppName) == "" {
		result.Error = "image tag and application name are required"
		return result, nil
	}
	workDir, workBranch, err := packager.Store.Prepare(ctx, req.Dir, req.RepoURL, req.Ref, req.Branch)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Branch = workBranch
	if strings.TrimSpace(req.Commit) != "" {
		if err := packager.Store.SelectCommit(ctx, workDir, req.Commit); err != nil {
			result.Error = err.Error()
			return result, nil
		}
	}
	facts := fmt.Sprintf("runtime=%s\nimageTag=%s\napp=%s", req.Runtime, req.ImageTag, req.AppName)
	input := domain.PackagingContext{
		RunID: req.AppName, ImageRef: req.BaseImage, ToolFacts: facts,
		IgnoreExistingDockerfile: true, Task: domain.TaskDockerfile,
	}.WithSkill(packagingskill.Source, packagingskill.SHA256())
	proposal, provenance, err := packager.Writer.ProposePackage(ctx, input)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	if strings.TrimSpace(proposal.Dockerfile) == "" {
		result.Error = "model did not write a Dockerfile"
		return result, nil
	}
	files := map[string]string{"Dockerfile": proposal.Dockerfile, ".dockerignore": proposal.Dockerignore}
	head, err := packager.Store.Commit(ctx, workDir, files, "chore(workspace): write Dockerfile")
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Head = head
	packager.streamf("committed Dockerfile %s on %s\n", head, workBranch)
	if reason := packager.Checks.Dockerfile(proposal.Dockerfile, proposal.Dockerignore); reason != "" {
		result.Outcome, result.Reason = string(domain.OutcomeFailRequired), reason
		result.Error = reason
		return result, nil
	}
	builtRef, err := packager.Builder.BuildImage(ctx, workDir, packager.LogWriter)
	if err != nil {
		result.Outcome = string(domain.OutcomeFailRequired)
		result.Reason = err.Error()
		result.Error = result.Reason
		return result, nil
	}
	pinnedRef := builtRef
	if publisher, ok := packager.Builder.(ImagePublisher); ok {
		pinnedRef, err = publisher.Publish(ctx, builtRef)
		if err != nil {
			result.Outcome = string(domain.OutcomeFailRequired)
			result.Reason = err.Error()
			result.Error = result.Reason
			return result, nil
		}
	}
	inspected, err := packager.Inspector.InspectImage(ctx, builtRef)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	name, digest, pinned := strings.Cut(pinnedRef, "@")
	if pinned && name != "" && digestPattern(digest) {
		inspected.Ref = pinnedRef
		inspected.Digest = digest
	}
	if !digestPattern(inspected.Digest) {
		result.Error = "image inspection returned no digest"
		return result, nil
	}
	result.ImageInspect = &inspected
	recorded := inspected.Digest
	if strings.Contains(inspected.Ref, "@sha256:") {
		recorded = inspected.Ref
	}
	packager.streamf("pinned image digest %s\n", recorded)
	packageInput := input
	packageInput.Task = domain.TaskPackage
	packageInput.ImageDigest = inspected.Digest
	packageInput.ImageRef = inspected.Ref
	packageProposal, packageProvenance, err := packager.Writer.ProposePackage(ctx, packageInput)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	if !strings.Contains(packageProposal.Zarf, inspected.Digest) || !strings.Contains(packageProposal.CR, inspected.Digest) {
		result.Error = "package files do not pin the observed image digest"
		return result, nil
	}
	if strings.Contains(recorded, "@sha256:") && !strings.Contains(packageProposal.Zarf, recorded) {
		result.Error = "package files do not reference the registry digest"
		return result, nil
	}
	if reason := packager.Checks.PackageCR(packageProposal.CR); reason != "" {
		result.Outcome, result.Reason = string(domain.OutcomeFailRequired), reason
		result.Error = reason
		return result, nil
	}
	head, err = packager.Store.Commit(ctx, workDir, map[string]string{
		"zarf.yaml": packageProposal.Zarf, "uds-package.yaml": packageProposal.CR,
		"package-source.yaml": packageProposal.Source,
	}, "chore(workspace): write zarf.yaml and Package CR")
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Head = head
	packager.streamf("committed package files %s\n", head)
	if err := packager.Checks.Lint(ctx, workDir); err != nil {
		result.Outcome, result.Reason = string(domain.OutcomeFailRequired), err.Error()
		result.Error = result.Reason
		return result, nil
	}
	packager.streamf("zarf dev lint passed\n")
	archive, err := packager.Checks.Create(ctx, workDir, workDir+"/out")
	if err != nil || strings.TrimSpace(archive) == "" {
		if err == nil {
			err = fmt.Errorf("archive was not created")
		}
		result.Outcome, result.Reason = string(domain.OutcomeFailRequired), err.Error()
		result.Error = result.Reason
		return result, nil
	}
	packager.streamf("zarf package create %s\n", archive)
	result.Proposal = packageProposal
	result.Proposal.Dockerfile = proposal.Dockerfile
	result.Proposal.Dockerignore = proposal.Dockerignore
	result.Provenance = packageProvenance
	result.Provenance.DockerfileSHA256 = domain.DigestText([]byte(proposal.Dockerfile))
	result.Provenance.ImageDigest = recorded
	result.Provenance.ResolvedCommit = head
	result.Provenance.RepoURL = req.RepoURL
	result.Provenance.ToolCalls = append(provenance.ToolCalls, domain.ToolCallRecord{Name: "inspect_image"})
	result.Archive = archive
	result.Outcome = string(domain.OutcomePass)
	result.Reason = "build, inspect, lint, and package create passed"
	result.Audit = buildWorkspaceAudit(result.Provenance, result.Outcome)
	if packager.Auditor != nil {
		if err := packager.Auditor.RecordWorkspacePackage(ctx, result.Audit); err != nil {
			return result, err
		}
	}
	return result, nil
}

func digestPattern(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
