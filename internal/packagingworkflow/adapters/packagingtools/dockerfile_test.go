package packagingtools

import (
	"context"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

const pinned = "registry.example/base/static-web@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const ignoreFile = ".git\nnode_modules\ndist\n.env\n*.pem\n"

func TestValidateDockerfileAcceptsAPinnedMultiStageImage(t *testing.T) {
	dockerfile := `
FROM ` + pinned + ` AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM ` + pinned + `
COPY --from=build --chown=65532:65532 /src/dist /srv
USER 65532:65532
EXPOSE 8080
CMD ["/usr/bin/http-server", "/srv"]
`
	report, err := New().ValidateDockerfile(context.Background(), dockerfile, ignoreFile)
	if err != nil || !report.OK {
		t.Fatalf("report = %#v %v", report, err)
	}
}

func TestValidateDockerfileRejectsUnpinnedRootSecretsAndPrivilegedPorts(t *testing.T) {
	dockerfile := `
FROM alpine:3.20
COPY .env /app/.env
ENV SECRET_TOKEN=supersecret
USER root
EXPOSE 80
`
	report, err := New().ValidateDockerfile(context.Background(), dockerfile, "")
	if err != nil || report.OK {
		t.Fatalf("expected errors, got %#v %v", report, err)
	}
	got := messages(report)
	for _, want := range []string{reasonDockerfileDigest, reasonDockerfileUser, reasonDockerfileSecret, reasonDockerfilePort, reasonDockerfileIgnore} {
		if !containsMessage(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestDryRunGatesFailsAPassingPackageWhenDockerfileBreaksConvention(t *testing.T) {
	source := []byte("name: static-web\nmemory: 128Mi\n")
	files, err := fixtures.Render(source, domain.DigestBytes(source), fixtures.TemplateVersion, fixtures.ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	result, err := New().DryRunGates(context.Background(), domain.GateInput{
		Source: string(source), Zarf: string(files.Zarf), CR: string(files.CR),
		Dockerfile: "FROM alpine:3.20\nUSER 65532\n", Dockerignore: ignoreFile,
	})
	if err != nil || result.Outcome != domain.OutcomeFailRequired || result.Reason != reasonDockerfileDigest {
		t.Fatalf("result = %#v %v", result, err)
	}
}

func TestDryRunGatesKeepsPackageFailureAheadOfDockerfile(t *testing.T) {
	result, err := New().DryRunGates(context.Background(), domain.GateInput{
		Source:     "name: static-web\nrequirement: unsupported\n",
		Dockerfile: "FROM alpine:3.20\n",
	})
	if err != nil || result.Outcome != domain.OutcomeUnsupported || result.Reason != "application requirement is unsupported" {
		t.Fatalf("result = %#v %v", result, err)
	}
}

func messages(report domain.ManifestReport) []string {
	var out []string
	for _, finding := range report.Findings {
		out = append(out, finding.Message)
	}
	return out
}

func containsMessage(got []string, want string) bool {
	for _, item := range got {
		if item == want {
			return true
		}
	}
	return strings.Contains(strings.Join(got, "\n"), want)
}
