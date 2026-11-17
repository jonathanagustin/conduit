package modelcli

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"strings"
	"testing"
)

func TestParseArgvDoesNotUseAShell(t *testing.T) {
	argv, err := ParseArgv("tool exec --flag \"quoted value\" -")
	if err != nil || len(argv) != 5 || argv[3] != "quoted value" {
		t.Fatalf("argv=%v err=%v", argv, err)
	}
	if _, err := ParseArgv("tool $(echo hi)"); err == nil {
		t.Fatal("accepted shell syntax")
	}
}
func TestLegacyRunnerRefusesBothModelPorts(t *testing.T) {
	runner := Runner{Argv: []string{"/bin/sh", "-c", "exit 0"}}
	_, _, err := runner.Propose(t.Context(), domain.PackagingContext{})
	if !errors.Is(err, localcmd.ErrLocalCommandUnsupported) || !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal(err)
	}
	_, _, err = runner.ProposePackage(t.Context(), domain.PackagingContext{})
	if !errors.Is(err, localcmd.ErrLocalCommandUnsupported) || !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal(err)
	}
}

type stubTools struct {
	ref string
}

func (stub *stubTools) InspectImage(_ context.Context, ref string) (domain.ImageInspect, error) {
	stub.ref = ref
	return domain.ImageInspect{Ref: ref, User: "65532", ExposedPorts: []string{"8080/tcp"}, Env: []string{"PORT"}}, nil
}
func (stub *stubTools) ValidateManifests(context.Context, string) (domain.ManifestReport, error) {
	return domain.ManifestReport{OK: true}, nil
}
func (stub *stubTools) DryRunGates(context.Context, domain.GateInput) (domain.Validation, error) {
	return domain.Validation{}, nil
}
func (stub *stubTools) InspectRepo(context.Context) (domain.RepoInspect, error) {
	return domain.RepoInspect{}, nil
}
func (stub *stubTools) ReadFile(context.Context, string) (domain.RepoFile, error) {
	return domain.RepoFile{}, nil
}
func (stub *stubTools) ValidateDockerfile(context.Context, string, string) (domain.ManifestReport, error) {
	return domain.ManifestReport{OK: true}, nil
}

func TestInspectFactsRemainsAvailableToHTTP(t *testing.T) {
	tools := &stubTools{}
	input := domain.PackagingContext{ImageRef: "fixture@sha256:" + strings.Repeat("ab", 32)}
	calls := InspectFacts(t.Context(), tools, &input)
	if tools.ref != input.ImageRef || len(calls) != 1 || calls[0].Name != "inspect_image" {
		t.Fatalf("ref=%q calls=%v", tools.ref, calls)
	}
	if !strings.Contains(input.ToolFacts, "65532") {
		t.Fatalf("facts=%q", input.ToolFacts)
	}
}
