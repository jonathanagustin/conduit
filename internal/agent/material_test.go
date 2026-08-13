package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestExternalMaterialPreparerSendsOnlyOpaqueMetadata(t *testing.T) {
	preparer, err := NewExternalMaterialPreparer("/opt/platform/bin/material-preparer")
	if err != nil {
		t.Fatal(err)
	}
	var command string
	var requests [][]byte
	preparer.run = func(_ context.Context, invoked string, payload []byte) error {
		command = invoked
		requests = append(requests, append([]byte(nil), payload...))
		return nil
	}
	if err := preparer.Prepare(context.Background(), controlplane.RegistryAccess{
		AssignmentID: "primary", Revision: 4, CredentialRef: "vault://registry",
	}, "example"); err != nil {
		t.Fatal(err)
	}
	access := controlplane.SecretAccess{
		Assignment: "database", Revision: 7, ProviderRef: "vault://database", Keys: []string{"password"},
	}
	if err := preparer.ApplySecretMutation(context.Background(), controlplane.SecretMutation{
		Action: controlplane.SecretCreate, Namespace: "example", Current: &access,
	}); err != nil {
		t.Fatal(err)
	}
	if err := preparer.PrepareImages(context.Background(), []controlplane.Artifact{{
		Repository: "registry.example/app", Tag: "1.0.0", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}, nil, "example"); err != nil {
		t.Fatal(err)
	}
	if command != "/opt/platform/bin/material-preparer" || len(requests) != 3 {
		t.Fatalf("external invocation = %q %#v", command, requests)
	}
	var envelope struct {
		APIVersion string          `json:"apiVersion"`
		Kind       string          `json:"kind"`
		Namespace  string          `json:"namespace"`
		Access     json.RawMessage `json:"access"`
	}
	if err := json.Unmarshal(requests[1], &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.APIVersion != "platform.delivery/v1alpha1" || envelope.Kind != "SecretMutation" || envelope.Namespace != "example" ||
		!strings.Contains(string(envelope.Access), `"providerRef":"vault://database"`) {
		t.Fatalf("request envelope = %#v payload=%s", envelope, requests[1])
	}
	for _, request := range requests {
		if strings.Contains(string(request), "secret-value") {
			t.Fatalf("request disclosed secret material: %s", request)
		}
	}
	if !strings.Contains(string(requests[2]), `"kind":"ImagePrePull"`) ||
		!strings.Contains(string(requests[2]), `"digest":"sha256:aaaaaaaa`) {
		t.Fatalf("image pre-pull request = %s", requests[2])
	}
}

func TestExternalMaterialPreparerValidatesCommandAndSuppressesProviderOutput(t *testing.T) {
	if _, err := NewExternalMaterialPreparer("relative/preparer"); err == nil {
		t.Fatal("relative command accepted")
	}
	preparer, err := NewExternalMaterialPreparer("/opt/platform/bin/material-preparer")
	if err != nil {
		t.Fatal(err)
	}
	preparer.run = func(context.Context, string, []byte) error { return errors.New("exit status 1") }
	access := controlplane.SecretAccess{Assignment: "database", Revision: 7}
	err = preparer.ApplySecretMutation(context.Background(), controlplane.SecretMutation{
		Action: controlplane.SecretVerify, Namespace: "example", Current: &access,
	})
	if err == nil || !strings.Contains(err.Error(), "external material preparer failed") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("provider error = %v", err)
	}
}

func TestNilExternalMaterialPreparerFailsClosedWithoutPanic(t *testing.T) {
	var preparer *ExternalMaterialPreparer
	err := preparer.PrepareImages(context.Background(), []controlplane.Artifact{{
		Repository: "registry.example/app", Tag: "1.0.0",
		Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}, nil, "example")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil preparer error = %v", err)
	}
}
