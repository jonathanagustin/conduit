package fixtures

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestExamplePackageMatchesGenerator(t *testing.T) {
	got, err := Render(Source, SourceDigest(), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	zarf, err := os.ReadFile("example-package/zarf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := os.ReadFile("example-package/uds-package.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Zarf, zarf) || !bytes.Equal(got.CR, cr) {
		t.Fatalf("example package drifted\n--- zarf ---\n%s\n--- cr ---\n%s", got.Zarf, got.CR)
	}
	again, err := Render(Source, SourceDigest(), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Zarf, again.Zarf) || !bytes.Equal(got.CR, again.CR) || got.Digest() != again.Digest() {
		t.Fatal("render is not deterministic")
	}
}

func TestUndeclaredNeedsUseDefaults(t *testing.T) {
	source := []byte("name: sample-app\nkind: fixture\n")
	got, err := Render(source, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	cr := string(got.CR)
	for _, want := range []string{
		`clientId: "sample-app"`,
		`redirectUris:`,
		`"https://sample-app.example/callback"`,
		`name: "sample-app-sso"`,
		`protocol: "openid-connect"`,
		`direction: "Egress"`,
		`remoteNamespace: "kube-system"`,
		`"k8s-app": "kube-dns"`,
		`port: 53`,
		`remoteProtocol: "UDP"`,
		`remoteProtocol: "TCP"`,
		`portName: "http"`,
		`targetPort: 8080`,
		`path: "/metrics"`,
		`mode: "ambient"`,
	} {
		if !strings.Contains(cr, want) {
			t.Fatalf("default package missing %s\n%s", want, cr)
		}
	}
	if strings.Contains(string(got.Zarf), "images:") {
		t.Fatalf("undeclared image was invented\n%s", got.Zarf)
	}
}

func TestDeclaredNeedsReplaceThatSectionOnly(t *testing.T) {
	source := []byte(strings.Join([]string{
		"name: declared-app",
		"port: 9090",
		"version: 1.2.3",
		"image: ghcr.io/example/declared-app:1.2.3@sha256:" + strings.Repeat("ab", 32),
		"sso: clientId=declared-app redirect=https://declared-app.example/oauth name=declared-app protocol=openid-connect secret=declared-app-client",
		"network: direction=Ingress remoteNamespace=istio-system remoteSelector=app=istio-ingressgateway port=9090 protocol=TCP description=gateway",
		"monitor: portName=metrics targetPort=9091 path=/stats",
		"mesh: sidecar",
	}, "\n") + "\n")
	got, err := Render(source, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	cr := string(got.CR)
	zarf := string(got.Zarf)
	for _, want := range []string{
		`clientId: "declared-app"`,
		`"https://declared-app.example/oauth"`,
		`name: "declared-app-client"`,
		`direction: "Ingress"`,
		`remoteNamespace: "istio-system"`,
		`"app": "istio-ingressgateway"`,
		`port: 9090`,
		`description: "gateway"`,
		`portName: "metrics"`,
		`targetPort: 9091`,
		`path: "/stats"`,
		`mode: "sidecar"`,
	} {
		if !strings.Contains(cr, want) {
			t.Fatalf("declared package missing %s\n%s", want, cr)
		}
	}
	for _, absent := range []string{
		"sample-app.example/callback",
		"kube-dns",
		"default-name-resolution",
		`path: "/metrics"`,
		`targetPort: 8080`,
	} {
		if strings.Contains(cr, absent) {
			t.Fatalf("declared package kept default %s\n%s", absent, cr)
		}
	}
	if !strings.Contains(zarf, `version: "1.2.3"`) || !strings.Contains(zarf, "ghcr.io/example/declared-app") {
		t.Fatalf("zarf.yaml = %s", zarf)
	}
}

func TestPartialDeclarationKeepsOtherDefaults(t *testing.T) {
	source := []byte("name: partial-app\nport: 9090\nsso: clientId=partial-app redirect=https://partial-app.example/back\n")
	got, err := Render(source, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	cr := string(got.CR)
	if strings.Contains(cr, "partial-app.example/callback") || !strings.Contains(cr, "https://partial-app.example/back") {
		t.Fatalf("sso default was not replaced\n%s", cr)
	}
	if !strings.Contains(cr, "kube-dns") || !strings.Contains(cr, `targetPort: 9090`) || !strings.Contains(cr, `path: "/metrics"`) {
		t.Fatalf("undeclared sections did not keep defaults\n%s", cr)
	}
}

func TestNeedsAreIndependentOfLineOrder(t *testing.T) {
	source := []byte(strings.Join([]string{
		"monitor: path=/health",
		"network: direction=Egress remoteNamespace=kube-system remoteSelector=k8s-app=kube-dns port=53 protocol=UDP",
		"port: 7070",
		"name: ordered-app",
	}, "\n") + "\n")
	got, err := Render(source, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	cr := string(got.CR)
	if !strings.Contains(cr, `"app.kubernetes.io/name": "ordered-app"`) || !strings.Contains(cr, `targetPort: 7070`) || !strings.Contains(cr, `path: "/health"`) {
		t.Fatalf("later declarations did not apply\n%s", cr)
	}
	if strings.Contains(cr, `remoteProtocol: "TCP"`) {
		t.Fatalf("declared network kept the default TCP rule\n%s", cr)
	}
	if !strings.Contains(cr, "https://ordered-app.example/callback") {
		t.Fatalf("undeclared sso was dropped\n%s", cr)
	}
}

func TestExplicitNoneKeepsEmptySections(t *testing.T) {
	source := []byte("name: quiet-app\nsso: none\nnetwork: none\nmonitor: none\n")
	got, err := Render(source, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	cr := string(got.CR)
	for _, want := range []string{"    allow: []\n", "  sso: []\n", "  monitor: []\n"} {
		if !strings.Contains(cr, want) {
			t.Fatalf("missing %q\n%s", want, cr)
		}
	}
	if strings.Contains(cr, "kube-dns") || strings.Contains(cr, "redirectUris:") || strings.Contains(cr, "portName:") {
		t.Fatalf("none still emitted entries\n%s", cr)
	}
}

func TestInvalidNeedsFailClosedWithoutEcho(t *testing.T) {
	secret := "SUPERSECRETVALUE"
	source := []byte("name: sample-app\nnetwork: direction=Sideways remoteNamespace=kube-system remoteSelector=k8s-app=kube-dns port=53 protocol=UDP token=" + secret + "\n")
	_, err := Render(source, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if !errors.Is(err, domain.ErrIdentity) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "Sideways") {
		t.Fatalf("error = %v", err)
	}
	got := Gate(source, nil, nil, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if got.Outcome != domain.OutcomeFailRequired || got.Reason != "declared needs are invalid" || strings.Contains(got.Reason, secret) {
		t.Fatalf("gate = %#v", got)
	}
}

func TestZarfDevLintAcceptsExample(t *testing.T) {
	bin := os.Getenv("ZARF_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("zarf")
		if err != nil {
			t.Skip("zarf is not installed")
		}
	}
	cmd := exec.Command(bin, "dev", "lint", "example-package", "--architecture", "amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zarf dev lint: %v\n%s", err, out)
	}
}
