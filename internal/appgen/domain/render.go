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
	Chart       []ChartFile
}

// Render builds the image definition, chart, and package documents from a
// validated spec.
//
// CONDUIT-INVENTED: appgen.HttpPackageTemplate
// Displaced-by: a reviewed packaging profile that states the image definition, chart values, and package document fields for a generated HTTP application.
func Render(spec Spec) (PackageFiles, error) {
	shape, reason, ok := ShapeOf(spec)
	if !ok {
		return PackageFiles{}, fmt.Errorf("%w: %s", ErrSpecInvalid, reason)
	}
	docker, err := dockerfile(spec, shape)
	if err != nil {
		return PackageFiles{}, err
	}
	values := chartValues(spec)
	chart, err := ChartFiles(spec.Name, ChartVersion, values)
	if err != nil {
		return PackageFiles{}, err
	}
	return PackageFiles{
		Dockerfile:  docker,
		ChartValues: values,
		Zarf:        zarfManifest(spec),
		Package:     packageCR(spec),
		Chart:       chart,
	}, nil
}

func dockerfile(spec Spec, shape Shape) ([]byte, error) {
	switch shape {
	case ShapeGo:
		return []byte(fmt.Sprintf(`FROM scratch
COPY app /app
USER 65532:65532
ENV PORT=%d
EXPOSE %d
ENTRYPOINT ["/app"]
`, spec.Port, spec.Port)), nil
	case ShapeHTML:
		return []byte(fmt.Sprintf(`FROM scratch
COPY app /app
COPY index.html /index.html
USER 65532:65532
ENV PORT=%d
EXPOSE %d
ENTRYPOINT ["/app"]
`, spec.Port, spec.Port)), nil
	case ShapePython:
		return []byte(fmt.Sprintf(`FROM %s
COPY main.py /app/main.py
USER 65532:65532
ENV PORT=%d
ENV PYTHONDONTWRITEBYTECODE=1
ENV HOME=/tmp
WORKDIR /app
EXPOSE %d
ENTRYPOINT ["python3", "-u", "/app/main.py"]
`, PythonRuntimeImage, spec.Port, spec.Port)), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrSpecInvalid, AllowedSourceShapes)
	}
}

func chartValues(spec Spec) []byte {
	memory := strings.TrimSpace(spec.Resources.Memory)
	cpu := strings.TrimSpace(spec.Resources.CPU)
	return []byte(fmt.Sprintf(`name: %s
port: %d
image: ""
resources:
  requests:
    memory: %s
    cpu: %s
  limits:
    memory: %s
    cpu: %s
`, spec.Name, spec.Port, memory, cpu, memory, cpu))
}

func zarfManifest(spec Spec) []byte {
	name := spec.Name
	return []byte(fmt.Sprintf(`apiVersion: zarf.dev/v1alpha1
kind: ZarfPackageConfig
metadata:
  name: "%s"
  description: "Generated HTTP application %s"
  version: "%s"
  annotations:
    packaging.conduit.dev/template: "http-app-1"
    packaging.conduit.dev/value-tier: "CONDUIT_NATIVE"
%scomponents:
  - name: "%s"
    required: true
    charts:
      - name: "%s"
        version: "%s"
        namespace: "%s"
        localPath: "chart"
    manifests:
      - name: "package"
        namespace: "%s"
        files:
          - "uds-package.yaml"
%s`, name, name, ChartVersion, fileDigestLines(spec), name, name, ChartVersion, name, name, imagePinUnset))
}

func packageCR(spec Spec) []byte {
	name := spec.Name
	return []byte(fmt.Sprintf(`apiVersion: uds.dev/v1alpha1
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
    allow:
      - direction: "Ingress"
        description: "kubelet probes"
        selector:
          "app.kubernetes.io/name": "%s"
        remoteGenerated: "KubeNodes"
        port: %d
    expose:
      - description: "http"
        service: "%s"
        host: "%s"
        selector:
          "app.kubernetes.io/name": "%s"
        port: %d
        targetPort: %d
  monitor:
    - portName: "http"
      targetPort: %d
      path: "/health"
      selector:
        "app.kubernetes.io/name": "%s"
`, name, name, spec.Port, fileDigestLines(spec), name, spec.Port, name, name, name, spec.Port, spec.Port, spec.Port, name))
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
