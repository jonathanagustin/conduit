package udsschema

import (
	"strings"
	"testing"
)

type corpusDocument struct {
	document string
	// rejected is empty for a valid document, otherwise a fragment of the
	// path the violation must be reported at.
	rejected string
}

const validPackage = `apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: excalidraw
  namespace: excalidraw
spec:
  network:
    expose:
      - service: excalidraw
        selector:
          app.kubernetes.io/name: excalidraw
        host: excalidraw
        gateway: tenant
        port: 80
        targetPort: 8080
    allow:
      - direction: Ingress
        selector:
          app.kubernetes.io/name: excalidraw
        remoteGenerated: IntraNamespace
  sso:
    - name: Excalidraw
      clientId: uds-excalidraw
      redirectUris:
        - https://excalidraw.uds.dev/login
`

var packageCorpus = map[string]corpusDocument{
	"valid":         {validPackage, ""},
	"minimal":       {"apiVersion: uds.dev/v1alpha1\nkind: Package\nmetadata: {name: a, namespace: a}\nspec: {}\n", ""},
	"with a status": {validPackage + "status:\n  phase: Ready\n  observedGeneration: 2\n", ""},
	// The cases a hand-written check of a few fields lets through.
	"misspelled spec field":    {strings.Replace(validPackage, "  network:", "  netwrok:", 1), "spec"},
	"misspelled allow field":   {strings.Replace(validPackage, "remoteGenerated:", "remoteGenrated:", 1), "spec.network.allow.0"},
	"unknown expose field":     {strings.Replace(validPackage, "gateway: tenant", "gateway: tenant\n        gatewayClass: x", 1), "spec.network.expose.0"},
	"unknown sso field":        {strings.Replace(validPackage, "clientId:", "clientID:", 1), "spec.sso.0"},
	"enum out of range":        {strings.Replace(validPackage, "IntraNamespace", "Sideways", 1), "spec.network.allow.0.remoteGenerated"},
	"direction out of range":   {strings.Replace(validPackage, "direction: Ingress", "direction: Inbound", 1), "spec.network.allow.0.direction"},
	"allow without direction":  {strings.Replace(validPackage, "      - direction: Ingress\n        selector:", "      - selector:", 1), "spec.network.allow.0"},
	"port as a string":         {strings.Replace(validPackage, "port: 80", "port: eighty", 1), "spec.network.expose.0.port"},
	"allow that is not a list": {"apiVersion: uds.dev/v1alpha1\nkind: Package\nmetadata: {name: a, namespace: a}\nspec: {network: {allow: {direction: Ingress}}}\n", "spec.network.allow"},
	"sso that is not a list":   {"apiVersion: uds.dev/v1alpha1\nkind: Package\nmetadata: {name: a, namespace: a}\nspec: {sso: {name: a}}\n", "spec.sso"},
}

func TestValidatePackageAgainstThePinnedSchema(t *testing.T) {
	t.Parallel()
	for name, testCase := range packageCorpus {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			violations := ValidatePackage([]byte(testCase.document))
			if testCase.rejected == "" {
				if len(violations) != 0 {
					t.Fatalf("a valid Package was refused: %v", violations)
				}
				return
			}
			for _, violation := range violations {
				if strings.HasPrefix(violation.Path, testCase.rejected) {
					return
				}
			}
			t.Fatalf("no violation at %s: %v", testCase.rejected, violations)
		})
	}
}

// The schema leaves the envelope open, so it is checked beside it.
func TestValidatePackageChecksTheEnvelope(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct{ document, path string }{
		"wrong apiVersion": {strings.Replace(validPackage, "uds.dev/v1alpha1", "uds.dev/v1beta1", 1), "apiVersion"},
		"wrong kind":       {strings.Replace(validPackage, "kind: Package", "kind: UDSPackage", 1), "kind"},
		"no namespace":     {strings.Replace(validPackage, "  namespace: excalidraw\n", "", 1), "metadata.namespace"},
		"no name":          {strings.Replace(validPackage, "  name: excalidraw\n  namespace", "  namespace", 1), "metadata.name"},
	} {
		violations := ValidatePackage([]byte(testCase.document))
		if len(violations) != 1 || violations[0].Path != testCase.path {
			t.Errorf("%s: violations = %v, want one at %s", name, violations, testCase.path)
		}
	}
	for name, document := range map[string]string{
		"empty": " \n", "not YAML": "a: [", "a list": "- a\n", "two documents": validPackage + "---\n" + validPackage,
	} {
		if violations := ValidatePackage([]byte(document)); len(violations) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}

const validBundle = `kind: UDSBundle
metadata:
  name: conduit
  description: Conduit runtime
  version: 0.18.0
  architecture: amd64
packages:
  - name: platform-runtime
    repository: registry.example/packages/platform-runtime
    ref: 0.18.0
    optionalComponents:
      - uds
    keylessVerification:
      certificateIdentityRegexp: ^https://github\.com/example/.*$
      certificateOIDCIssuerRegexp: ^https://token\.actions\.githubusercontent\.com$
`

var bundleCorpus = map[string]corpusDocument{
	"valid":      {validBundle, ""},
	"local path": {"kind: UDSBundle\nmetadata: {name: conduit}\npackages:\n  - {name: a, path: ., ref: 0.1.0}\n", ""},
	// The shape udsconverge used to render, which uds-cli v0.41.0 does not read.
	"an apiVersion":           {"apiVersion: uds.dev/v1alpha1\n" + validBundle, ""},
	"zarf-packages":           {strings.Replace(validBundle, "packages:", "zarf-packages:", 1), ""},
	"optional-components":     {strings.Replace(validBundle, "optionalComponents:", "optional-components:", 1), "packages.0"},
	"annotations in metadata": {strings.Replace(validBundle, "  architecture: amd64\n", "  architecture: amd64\n  annotations: {a: b}\n", 1), "metadata"},
	"package without a ref":   {strings.Replace(validBundle, "    ref: 0.18.0\n", "", 1), "packages.0"},
	"wrong kind":              {strings.Replace(validBundle, "kind: UDSBundle", "kind: Bundle", 1), "kind"},
	"name with a capital":     {strings.Replace(validBundle, "name: conduit", "name: Conduit", 1), "metadata.name"},
	"unknown keyless field":   {strings.Replace(validBundle, "certificateIdentityRegexp", "certificateIdentityPattern", 1), "packages.0.keylessVerification"},
}

func TestValidateBundleAgainstThePinnedSchema(t *testing.T) {
	t.Parallel()
	for name, testCase := range bundleCorpus {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			violations := ValidateBundle([]byte(testCase.document))
			switch {
			case name == "an apiVersion" || name == "zarf-packages":
				// Both are refused at the document root: an unknown top-level
				// key, and for zarf-packages a missing required one as well.
				if len(violations) == 0 {
					t.Fatalf("the retired bundle shape was accepted")
				}
			case testCase.rejected == "":
				if len(violations) != 0 {
					t.Fatalf("a valid bundle was refused: %v", violations)
				}
			default:
				for _, violation := range violations {
					if strings.HasPrefix(violation.Path, testCase.rejected) {
						return
					}
				}
				t.Fatalf("no violation at %s: %v", testCase.rejected, violations)
			}
		})
	}
}

// The frozen legacy bundle remains a reference for generated YAML consumers.
// The owned native HCL bundle is qualified separately with the pinned CLI.
func TestTheLegacyReferenceBundleValidates(t *testing.T) {
	t.Parallel()
	if violations := ValidateBundle(readRepositoryFile(t, "internal/udsschema/testdata/legacy-runtime-bundle.yaml")); len(violations) != 0 {
		t.Fatalf("internal/udsschema/testdata/legacy-runtime-bundle.yaml: %v", violations)
	}
}
