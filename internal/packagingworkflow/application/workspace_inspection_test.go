package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type inspectionFunc func(context.Context, string) (domain.ImageInspect, error)

func (f inspectionFunc) InspectImage(ctx context.Context, ref string) (domain.ImageInspect, error) {
	return f(ctx, ref)
}

type imageBuildFunc func(context.Context, string, io.Writer) (string, error)

func (f imageBuildFunc) BuildImage(ctx context.Context, dir string, out io.Writer) (string, error) {
	return f(ctx, dir, out)
}

type createRetryChecks struct{ stubChecks }

func (c *createRetryChecks) Create(context.Context, string, string) (string, error) {
	c.creates++
	return "", fmt.Errorf("package creation failed")
}

func inspectionProposal() domain.PackageProposal {
	return domain.PackageProposal{Source: "name: web\n", Zarf: "zarf", CR: "package", Dockerfile: "FROM scratch\nUSER 65532\n"}
}
func TestConfiguredInspectionFailureCannotPassPackaging(t *testing.T) {
	for _, mode := range []string{"error", "missing digest"} {
		t.Run(mode, func(t *testing.T) {
			checks, audit := &stubChecks{}, &stubWorkspaceAuditor{}
			built := "ghcr.io/example/built:v2"
			inspector := inspectionFunc(func(_ context.Context, ref string) (domain.ImageInspect, error) {
				if ref != built {
					t.Fatalf("inspected %q, wanted built reference %q", ref, built)
				}
				if mode == "error" {
					return domain.ImageInspect{}, fmt.Errorf("registry unavailable")
				}
				return domain.ImageInspect{Ref: ref}, nil
			})
			result, err := (WorkspacePackager{Writer: &stubWriter{proposals: []domain.PackageProposal{inspectionProposal()}}, Store: &stubStore{}, Checks: checks, Auditor: audit, Inspector: inspector, Builder: imageBuildFunc(func(context.Context, string, io.Writer) (string, error) { return built, nil })}).WritePackage(t.Context(), "/tmp/ws", "https://git.example/app", "main", "workspace/test", domain.PackagingContext{ImageRef: "ghcr.io/example/input:v1", ImageDigest: "sha256:" + strings.Repeat("3", 64)})
			if err != nil || result.Outcome != string(domain.OutcomeFailRequired) || len(result.Attempts) != MaxPackageRepairs+1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if checks.creates != 0 || result.Archive != "" || result.ImageInspect != nil || result.Provenance.ImageDigest != "" {
				t.Fatalf("failed inspection acknowledged artifact: %+v creates=%d", result, checks.creates)
			}
			if len(audit.records) != 1 || audit.records[0].Outcome != string(domain.OutcomeFailRequired) || audit.records[0].ImageDigest != "" {
				t.Fatalf("failure audit=%+v", audit.records)
			}
			for _, attempt := range result.Attempts {
				if attempt.Outcome != string(domain.OutcomeFailRequired) || !strings.Contains(attempt.Reason, "inspect") {
					t.Fatalf("attempt=%+v", attempt)
				}
			}
		})
	}
}
func TestInspectionRetryUsesOnlyCurrentBuildEvidence(t *testing.T) {
	calls := 0
	checks, audit := &stubChecks{}, &stubWorkspaceAuditor{}
	result, err := (WorkspacePackager{Writer: &stubWriter{proposals: []domain.PackageProposal{inspectionProposal()}}, Store: &stubStore{}, Checks: checks, Auditor: audit,
		Builder: imageBuildFunc(func(context.Context, string, io.Writer) (string, error) {
			return fmt.Sprintf("ghcr.io/example/built:v%d", calls+1), nil
		}),
		Inspector: inspectionFunc(func(_ context.Context, ref string) (domain.ImageInspect, error) {
			calls++
			if calls == 1 {
				return domain.ImageInspect{}, fmt.Errorf("temporary inspection failure")
			}
			return domain.ImageInspect{Ref: ref, Digest: "sha256:" + strings.Repeat("8", 64)}, nil
		}),
	}).WritePackage(t.Context(), "/tmp/ws", "https://git.example/app", "main", "workspace/test", domain.PackagingContext{ImageRef: "ghcr.io/example/input:v0"})
	if err != nil || result.Outcome != string(domain.OutcomePass) || len(result.Attempts) != 2 || checks.creates != 1 {
		t.Fatalf("result=%+v creates=%d err=%v", result, checks.creates, err)
	}
	if result.ImageInspect == nil || result.ImageInspect.Ref != "ghcr.io/example/built:v2" || result.Audit.ImageDigest != result.ImageInspect.Digest || result.Audit.Outcome != string(domain.OutcomePass) {
		t.Fatalf("wrong successful evidence: %+v", result)
	}
}
func TestLaterFailedBuildCannotRetainPriorInspection(t *testing.T) {
	calls := 0
	checks := &createRetryChecks{}
	result, err := (WorkspacePackager{Writer: &stubWriter{proposals: []domain.PackageProposal{inspectionProposal()}}, Store: &stubStore{}, Checks: checks,
		Builder: imageBuildFunc(func(context.Context, string, io.Writer) (string, error) {
			calls++
			if calls > 1 {
				return "", fmt.Errorf("later build failed")
			}
			return "ghcr.io/example/first:v1", nil
		}),
		Inspector: inspectionFunc(func(_ context.Context, ref string) (domain.ImageInspect, error) {
			return domain.ImageInspect{Ref: ref, Digest: "sha256:" + strings.Repeat("8", 64)}, nil
		}),
	}).WritePackage(t.Context(), "/tmp/ws", "https://git.example/app", "main", "workspace/test", domain.PackagingContext{})
	if err != nil || result.Outcome != string(domain.OutcomeFailRequired) || result.ImageInspect != nil || checks.creates != 1 {
		t.Fatalf("stale evidence: %+v creates=%d err=%v", result, checks.creates, err)
	}
}

func TestOptionalInspectionPreservesDeclaredImage(t *testing.T) {
	digest := "sha256:" + strings.Repeat("3", 64)
	result, err := (WorkspacePackager{Writer: &stubWriter{proposals: []domain.PackageProposal{inspectionProposal()}}, Store: &stubStore{}, Checks: &stubChecks{}}).WritePackage(t.Context(), "/tmp/ws", "https://git.example/web.git", "main", "workspace/demo", domain.PackagingContext{ImageRef: "ghcr.io/example/input:v1", ImageDigest: digest})
	if err != nil || result.Outcome != string(domain.OutcomePass) || result.ImageInspect != nil || result.Audit.ImageDigest != digest {
		t.Fatalf("optional inspection changed: %+v %v", result, err)
	}
}
