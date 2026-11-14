package packagingtools

import "testing"

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
