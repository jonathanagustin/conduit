package packagingtools

import (
	"context"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

func TestDryRunGatesUsesTheRealPackageGate(t *testing.T) {
	source := []byte("name: static-web\nmemory: 128Mi\n")
	files, err := fixtures.Render(source, domain.DigestBytes(source), fixtures.TemplateVersion, fixtures.ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	passed, err := New().DryRunGates(context.Background(), domain.GateInput{
		Source: string(source), Zarf: string(files.Zarf), CR: string(files.CR),
	})
	if err != nil || passed.Outcome != domain.OutcomePass {
		t.Fatalf("pass = %#v %v", passed, err)
	}
	blocked, err := New().DryRunGates(context.Background(), domain.GateInput{
		Source: "name: static-web\nmemory: 8Gi\n", Zarf: string(files.Zarf), CR: string(files.CR),
	})
	if err != nil || blocked.Outcome != domain.OutcomeFailRequired || blocked.Reason != "memory limit 8Gi exceeds the 1Gi ceiling" {
		t.Fatalf("blocked = %#v %v", blocked, err)
	}
	unsupported, err := New().DryRunGates(context.Background(), domain.GateInput{
		Source: "name: static-web\nrequirement: unsupported\n",
	})
	if err != nil || unsupported.Outcome != domain.OutcomeUnsupported || unsupported.Reason != "application requirement is unsupported" {
		t.Fatalf("unsupported = %#v %v", unsupported, err)
	}
}
