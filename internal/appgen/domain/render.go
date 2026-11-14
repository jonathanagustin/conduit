package domain

import (
	"fmt"
	"sort"
	"strings"
)

// PackageFiles is the packaging the Hub renders. A generator does not write these.
type PackageFiles struct {
	Dockerfile  []byte
	ChartValues []byte
	Zarf        []byte
	Package     []byte
}

// Render builds the image definition, chart values, and package documents
// from a validated spec.
//
// CONDUIT-INVENTED: appgen.HttpPackageTemplate
// Displaced-by: a reviewed packaging profile that states the image definition, chart values, and package document fields for a generated HTTP application.
func Render(spec Spec) (PackageFiles, error) {
	if _, ok := CheckAllowlist(spec); !ok {
		return PackageFiles{}, fmt.Errorf("%w: source files are not on the allowlist", ErrSpecInvalid)
	}
	memory := strings.TrimSpace(spec.Resources.Memory)
	cpu := strings.TrimSpace(spec.Resources.CPU)
	name := spec.Name
	files := PackageFiles{
		Dockerfile: []byte(fmt.Sprintf(`FROM scratch
COPY app /app
USER 65532:65532
ENV PORT=%d
EXPOSE %d
ENTRYPOINT ["/app"]
`, spec.Port, spec.Port)),
		ChartValues: []byte(fmt.Sprintf(`name: %s
port: %d
resources:
  requests:
    memory: %s
    cpu: %s
  limits:
    memory: %s
    cpu: %s
`, name, spec.Port, memory, cpu, memory, cpu)),
	}
	digests := fileDigestLines(spec)
	files.Zarf = []byte(fmt.Sprintf(`apiVersion: zarf.dev/v1alpha1
kind: ZarfPackageConfig
metadata:
  name: "%s"
  description: "Generated HTTP application %s"
  version: "0.1.0"
  annotations:
    packaging.conduit.dev/template: "http-app-1"
    packaging.conduit.dev/value-tier: "CONDUIT_NATIVE"
%scomponents:
  - name: "%s"
    required: true
    manifests:
      - name: "package"
        namespace: "%s"
        files:
          - "uds-package.yaml"
`, name, name, digests, name, name))
	files.Package = []byte(fmt.Sprintf(`apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: "%s"
  namespace: "%s"
  annotations:
    packaging.conduit.dev/template: "http-app-1"
    packaging.conduit.dev/value-tier: "CONDUIT_NATIVE"
    packaging.conduit.dev/port: "%d"
%sspec:
  network:
    allow: []
  monitor:
    - portName: "http"
      targetPort: %d
      path: "/health"
`, name, name, spec.Port, digests, spec.Port))
	return files, nil
}

// PackagingSource is the text the existing packaging run reads.
func PackagingSource(spec Spec, imageRef, imageDigest, rationale string) []byte {
	memory := strings.TrimSpace(spec.Resources.Memory)
	return []byte(fmt.Sprintf(`name: %s
kind: fixture
proposal: memory limit %s
rationale: %s
memory-request: %s
memory: %s
chart-values: memory: %s
resources: limits.memory=%s requests.memory=%s
image-ref: %s
image-digest: %s
port: %d
version: 0.1.0
sso: none
network: none
monitor: none
`, spec.Name, memory, oneLine(rationale), memory, memory, memory, memory, memory, oneLine(imageRef), oneLine(imageDigest), spec.Port))
}

func fileDigestLines(spec Spec) string {
	lines := make([]string, 0, len(spec.Files))
	files := append([]SourceFile(nil), spec.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, file := range files {
		lines = append(lines, fmt.Sprintf("    packaging.conduit.dev/file-%s: \"%s\"\n", strings.ReplaceAll(file.Path, ".", "-"), Digest([]byte(file.Content))))
	}
	return strings.Join(lines, "")
}
