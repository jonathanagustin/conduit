package uds

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestPinnedCLINativeBundleValuesPreserveReviewedTypes(t *testing.T) {
	if os.Getenv("CONDUIT_TEST_UDS_EXECUTOR_CLI") != "1" {
		t.Skip("set CONDUIT_TEST_UDS_EXECUTOR_CLI=1 with pinned CLIs")
	}
	zarf, uds := os.Getenv("ZARF_BIN"), os.Getenv("UDS_BIN")
	if zarf == "" || uds == "" {
		t.Fatal("explicit pinned CLIs required")
	}
	root := t.TempDir()
	t.Setenv("KUBECONFIG", filepath.Join(root, "no-cluster"))
	observed := filepath.Join(root, "observed.json")
	// Action-only fixture: no image, chart, manifest, wait or Kubernetes command.
	definition := fmt.Sprintf(`apiVersion: zarf.dev/v1beta1
kind: ZarfPackageConfig
metadata:
  name: native-bundle-values-fixture
  version: 0.0.0
values:
  files: [values.yaml]
components:
  - name: capture
    actions:
      onDeploy:
        before:
          - cmd: >-
              printf '%%s' '{{ .Values | toJson | b64enc }}' | base64 -d > %q
            enableTemplating: true
`, observed)
	writeCLIFile(t, filepath.Join(root, "zarf.yaml"), []byte(definition))
	writeCLIFile(t, filepath.Join(root, "values.yaml"), []byte("{}\n"))
	writeCLIFile(t, filepath.Join(root, "bundle.uds.hcl"), []byte(fmt.Sprintf(`uds { bundle_api_version = "uds.dev/v1alpha1" }
metadata {
  name = "native-bundle-values-fixture"
  version = "0.0.0"
}
package "capture" {
  source = "./zarf-package-native-bundle-values-fixture-%s-0.0.0.tar.zst"
  signature_verification { verify = false }
  values_files = ["reviewed-values.yaml"]
}
`, runtime.GOARCH)))
	writeCLIFile(t, filepath.Join(root, "reviewed-values.yaml"), []byte(`{{ range $key, $value := .vars }}
{{ printf "%q" $key }}: {{ if eq (printf "%T" $value) "string" }}{{ printf "%q" $value }}{{ else }}{{ $value }}{{ end }}
{{ end }}
`))
	step := controlplane.PlanStep{Values: json.RawMessage(`{"TEXT":"0012","TEXT_FALSE":"false","BOOL":false,"COUNT":12,"LITERAL":"${1+1} %{literal} 東京"}`)}
	configuration, err := nativeBundleConfiguration(step)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "config.uds.hcl")
	writeCLIFile(t, config, configuration)
	run := func(binary string, args ...string) {
		t.Helper()
		out, err := (scopedRunner{}).Run(t.Context(), binary, args...)
		if err != nil {
			t.Fatalf("native CLI: %v\n%s", err, out)
		}
	}
	run(zarf, "package", "create", root, "--confirm", "--skip-sbom", "--architecture", runtime.GOARCH, "--output", root)
	run(uds, "--features", "NextMode", "bundle", "create", root, "--unsigned", "--tmp-dir", root)
	archive := filepath.Join(root, "uds-bundle-native-bundle-values-fixture-"+runtime.GOARCH+"-0.0.0.tar.zst")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := filepath.Join(root, "publisher.pub")
	writePublicKey(t, publicKey, &key.PublicKey)
	if out, err := (scopedRunner{}).Run(t.Context(), uds, "--features", "NextMode", "bundle", "verify", archive, "--public-key", publicKey, "--tmp-dir", root); err == nil || !strings.Contains(string(out), "not signed") {
		t.Fatalf("native verification admitted unsigned fixture: %v\n%s", err, out)
	}
	// Signature skipping is confined to this unsigned, local action-only
	// fixture. Production commands require the admitted public key twice.
	run(uds, "--features", "NextMode", "bundle", "deploy", archive, "--skip-signature-verification", "--config", config, "--tmp-dir", root)
	data, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(step.Values, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native UDS changed reviewed types/literals: got %s want %s", data, step.Values)
	}
}
