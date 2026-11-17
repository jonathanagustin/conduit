// Package deterministic writes package files without a live model process.
// A configured model command replaces it. The files are committed by the caller.
package deterministic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// Writer is the offline packaging proposer.
type Writer struct{}

// ProposePackage returns a Dockerfile, or package files that name an observed digest.
func (Writer) ProposePackage(_ context.Context, input domain.PackagingContext) (domain.PackageProposal, domain.Provenance, error) {
	app := appName(input)
	tag := fact(input, "imageTag")
	if tag == "" {
		tag = "conduit/" + app + ":local"
	}
	digest := strings.TrimSpace(input.ImageDigest)
	proposal := domain.PackageProposal{
		Rationale: "Workspace Dockerfile, zarf.yaml, and Package CR for " + app + ".",
	}
	if digest == "" {
		proposal.Dockerfile, proposal.Dockerignore = dockerfile(input)
		proposal.Source = "name: " + app + "\nversion: 0.1.0\nport: 8080\n"
	} else {
		if !strings.HasPrefix(digest, "sha256:") {
			return domain.PackageProposal{}, domain.Provenance{}, fmt.Errorf("image digest is not pinned")
		}
		proposal.Source = fmt.Sprintf("name: %s\nversion: 0.1.0\nport: 8080\nimage-digest: %s\n", app, digest)
		proposal.Zarf = zarfYAML(app, tag, digest)
		proposal.CR = packageCR(app, digest)
		proposal.Dockerignore = dockerignore()
	}
	raw, err := json.Marshal(proposal)
	if err != nil {
		return domain.PackageProposal{}, domain.Provenance{}, err
	}
	return proposal, domain.Provenance{
		Adapter:        "deterministic",
		PromptSHA256:   domain.DigestText([]byte(domain.Prompt(input))),
		ResponseSHA256: domain.DigestText(raw),
		ToolCalls:      []domain.ToolCallRecord{{Name: "deterministic"}},
	}, nil
}

func dockerfile(input domain.PackagingContext) (string, string) {
	if fact(input, "runtime") == "node" && strings.Contains(input.ImageRef, "@sha256:") {
		base := strings.TrimSpace(input.ImageRef)
		body := strings.Join([]string{
			"FROM " + base + " AS build",
			"WORKDIR /src",
			"COPY . .",
			"RUN corepack enable && corepack prepare yarn@1.22.22 --activate && yarn install --frozen-lockfile --ignore-scripts && yarn build && mkdir -p /out && if [ -d excalidraw-app/build ]; then cp -a excalidraw-app/build/. /out/; elif [ -d build ]; then cp -a build/. /out/; else echo 'application build output was not produced' >&2; exit 1; fi",
			"FROM scratch",
			"COPY --from=build /out /srv",
			"USER 65532:65532",
			"EXPOSE 8080",
			"",
		}, "\n")
		return body, dockerignore()
	}
	body := strings.Join([]string{
		"FROM scratch AS source",
		"COPY source.txt /source.txt",
		"FROM scratch",
		"COPY --from=source /source.txt /source.txt",
		"USER 65532:65532",
		"EXPOSE 8080",
		"",
	}, "\n")
	return body, dockerignore()
}

func dockerignore() string {
	return ".git\nout\n*.tar.zst\nnode_modules\n.env\n"
}

func zarfYAML(app, tag, digest string) string {
	return fmt.Sprintf(`apiVersion: zarf.dev/v1beta1
kind: ZarfPackageConfig
metadata:
  name: %q
  description: "Governed package for %s"
  version: "0.1.0"
  annotations:
    packaging.conduit.dev/image-digest: %q
    packaging.conduit.dev/value-tier: "CONDUIT_NATIVE"
components:
  - name: %q
    optional: false
    images:
      - name: %q
        source: daemon
    manifests:
      - name: "uds-package"
        namespace: %q
        files:
          - "uds-package.yaml"
`, app, app, digest, app, tag, app)
}

func packageCR(app, digest string) string {
	return fmt.Sprintf(`apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: %q
  namespace: %q
  annotations:
    packaging.conduit.dev/image-digest: %q
    packaging.conduit.dev/value-tier: "CONDUIT_NATIVE"
spec:
  network:
    allow: []
  sso: []
`, app, app, digest)
}

func appName(input domain.PackagingContext) string {
	if name := fact(input, "app"); name != "" {
		return name
	}
	return "fixture"
}

func fact(input domain.PackagingContext, key string) string {
	for _, line := range strings.Split(input.ToolFacts, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && name == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
