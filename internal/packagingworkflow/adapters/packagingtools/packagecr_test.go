package packagingtools

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/udsschema"
)

const schemaValidPackageCR = `apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: static-web
  namespace: static-web
spec:
  network:
    allow:
      - direction: Ingress
        selector:
          app.kubernetes.io/name: static-web
        remoteGenerated: IntraNamespace
    expose:
      - service: static-web
        selector:
          app.kubernetes.io/name: static-web
        host: static-web
        gateway: tenant
        port: 8080
  monitor:
    - selector:
        app.kubernetes.io/name: static-web
      portName: http
      targetPort: 8080
`

// Every one of these passed the gate while it looked at a handful of fields by
// hand: the document has the right apiVersion, kind, name, namespace, a
// network, and lists where lists were checked. UDS Core would refuse each.
func TestValidatePackageCRRejectsWhatThePinnedSchemaRejects(t *testing.T) {
	if report := validatePackageCR(schemaValidPackageCR); !report.OK {
		t.Fatalf("the unmodified Package was refused: %#v", report.Findings)
	}
	for name, testCase := range map[string]struct{ from, to, path string }{
		"misspelled spec field":     {"  monitor:", "  monitors:", "spec"},
		"misspelled network field":  {"    expose:", "    exposes:", "spec.network"},
		"misspelled allow field":    {"remoteGenerated:", "remoteGenrated:", "spec.network.allow.0"},
		"unknown expose field":      {"gateway: tenant", "gatewayClass: tenant", "spec.network.expose.0"},
		"value outside the enum":    {"IntraNamespace", "SameNamespace", "spec.network.allow.0.remoteGenerated"},
		"monitor without selector":  {"    - selector:\n        app.kubernetes.io/name: static-web\n      portName", "    - portName", "spec.monitor.0"},
		"port that is not a number": {"port: 8080\n  monitor", "port: http\n  monitor", "spec.network.expose.0.port"},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := strings.Replace(schemaValidPackageCR, testCase.from, testCase.to, 1)
			if mutated == schemaValidPackageCR {
				t.Fatal("the mutation changed nothing")
			}
			report := validatePackageCR(mutated)
			if report.OK {
				t.Fatalf("accepted:\n%s", mutated)
			}
			for _, finding := range report.Findings {
				if finding.Path == testCase.path && finding.Rule == rulePackageCR && finding.Severity == sevError &&
					strings.Contains(finding.Message, "UDS Core "+udsschema.CoreVersion+" Package schema") {
					return
				}
			}
			t.Fatalf("no schema finding at %s: %#v", testCase.path, report.Findings)
		})
	}
}

// A second document in the file would never have been looked at.
func TestValidatePackageCRRejectsASecondDocument(t *testing.T) {
	if report := validatePackageCR(schemaValidPackageCR + "---\n" + schemaValidPackageCR); report.OK {
		t.Fatal("a two-document Package file was accepted on the strength of its first document")
	}
}

func TestValidatePackageCRAcceptsAMinimalPackage(t *testing.T) {
	raw := `apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: static-web
  namespace: static-web
spec:
  network:
    allow: []
`
	report := validatePackageCR(raw)
	if !report.OK {
		t.Fatalf("report = %#v", report)
	}
}

func TestValidatePackageCRRejectsMissingNetworkAndSSOShape(t *testing.T) {
	raw := `apiVersion: v1
kind: ConfigMap
metadata:
  name: static-web
spec:
  sso: "yes"
`
	report := validatePackageCR(raw)
	if report.OK {
		t.Fatal("expected schema errors")
	}
	got := messages(report)
	for _, want := range []string{reasonPackageAPI, reasonPackageKind, reasonPackageNamespace, reasonPackageNetwork, reasonPackageSSO} {
		if !containsMessage(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}
