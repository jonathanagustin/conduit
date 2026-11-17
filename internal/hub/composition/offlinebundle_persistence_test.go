package composition

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	offlineapp "github.com/jonathanagustin/conduit/internal/delivery/offlinebundle/application"
)

func buildOfflineFileBundle(t *testing.T) (string, ed25519.PrivateKey, controlplane.BundleEnvelope) {
	t.Helper()
	root := t.TempDir()
	repository, err := controlplane.NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlplane.NewLifecycle(repository).CreatePlan(t.Context(), desiredState("offline-site", "app.example:offline")); err != nil {
		t.Fatal(err)
	}
	key, err := controlplane.GenerateBundleSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	service, err := controlplane.NewBundleService(repository, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := service.PutPipeline(t.Context(), controlplane.ExportPipeline{
		ID: "offline-file", DisplayName: "Offline file", BuildStrategy: controlplane.BundleStrategySnapshot,
		Selections: []controlplane.BundleSelectionRule{{Type: controlplane.BundleSelectEnvironment, IDs: []string{"offline-site"}, ReleaseStrategy: controlplane.BundleReleasesAll}},
	})
	if err != nil {
		t.Fatal(err)
	}
	built, err := service.Build(t.Context(), pipeline.ID)
	if err != nil || len(built) != 1 || len(built[0].Manifest.Resources) == 0 {
		t.Fatalf("build = %#v, %v", built, err)
	}
	return root, key, built[0]
}

func offlineFileHub(t *testing.T, root string, key ed25519.PrivateKey) http.Handler {
	t.Helper()
	repository, err := controlplane.NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		BundlePrivateKey: key, AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestOfflineBundleFilePersistenceHandoffMatchesHTTPVerification(t *testing.T) {
	root, key, built := buildOfflineFileBundle(t)
	handler := offlineFileHub(t, root, key)
	request := httptest.NewRequest(http.MethodGet, "/v1/bundles/"+built.Manifest.ID, nil)
	request.Header.Set("X-Platform-Subject", "root")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stored envelope = %d %s", response.Code, response.Body.String())
	}
	verified := postAssistant(handler, "root", "", "/v1/bundles/verification", response.Body.String())
	var preview controlplane.BundlePreview
	if err := json.Unmarshal(verified.Body.Bytes(), &preview); err != nil || verified.Code != http.StatusOK ||
		!preview.Signature.Verified || preview.Signature.Status != controlplane.SignatureVerified {
		t.Fatalf("HTTP verification = %d %s, %v", verified.Code, verified.Body.String(), err)
	}
	handoff := postAssistant(handler, "root", "", "/v1/offline-bundles",
		`{"action":"handoff","bundleId":"`+built.Manifest.ID+`","site":"offline-site","confirm":true}`)
	var result struct {
		Handoff offlineapp.Handoff `json:"handoff"`
	}
	if err := json.Unmarshal(handoff.Body.Bytes(), &result); err != nil || handoff.Code != http.StatusCreated ||
		result.Handoff.BundleID != built.Manifest.ID || result.Handoff.Digest != built.ManifestDigest ||
		result.Handoff.Status != "handed_off" || result.Handoff.TakenBy != "root" {
		t.Fatalf("stored Bundle handoff = %d %s, %v", handoff.Code, handoff.Body.String(), err)
	}
	reported := postAssistant(handler, "root", "", "/v1/offline-bundles",
		`{"action":"report","id":"`+result.Handoff.ID+`","outcome":"installed"}`)
	if err := json.Unmarshal(reported.Body.Bytes(), &result); err != nil || reported.Code != http.StatusAccepted ||
		result.Handoff.Status != "pending_check_in" {
		t.Fatalf("offline report must still await real Agent presence = %d %s, %v", reported.Code, reported.Body.String(), err)
	}
}

func TestOfflineBundleFilePersistenceRefusesInvalidEvidence(t *testing.T) {
	_, key, built := buildOfflineFileBundle(t)
	otherKey, err := controlplane.GenerateBundleSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*controlplane.BundleEnvelope)
		key    ed25519.PrivateKey
		status string
	}{
		{name: "resource", change: func(bundle *controlplane.BundleEnvelope) {
			bundle.Manifest.Resources[0].Payload = json.RawMessage(`{"tampered":true}`)
		}},
		{name: "manifest", change: func(bundle *controlplane.BundleEnvelope) { bundle.Manifest.TargetHub = "tampered-target" }},
		{name: "signature", change: func(bundle *controlplane.BundleEnvelope) { bundle.Signature = "AAAA" }},
		{name: "untrusted", key: otherKey},
		{name: "unsigned", change: func(bundle *controlplane.BundleEnvelope) { bundle.Signature = "" }, status: controlplane.SignatureUnsigned},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(built)
			if err != nil {
				t.Fatal(err)
			}
			var envelope controlplane.BundleEnvelope
			if err := json.Unmarshal(encoded, &envelope); err != nil {
				t.Fatal(err)
			}
			if test.change != nil {
				test.change(&envelope)
			}
			root := t.TempDir()
			repository, err := controlplane.NewFileRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repository.CreateBundle(t.Context(), envelope); err != nil {
				t.Fatal(err)
			}
			verifyKey := key
			if test.key != nil {
				verifyKey = test.key
			}
			handler := offlineFileHub(t, root, verifyKey)
			if test.status == "" {
				test.status = controlplane.SignatureFailed
			}
			stored, err := repository.GetBundle(t.Context(), envelope.Manifest.ID)
			if err != nil {
				t.Fatal(err)
			}
			service, err := controlplane.NewBundleService(repository, verifyKey, nil)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := service.Verification(t.Context(), stored)
			if err != nil || preview.Signature.Verified || preview.Signature.Status != test.status {
				t.Fatalf("stored invalid evidence was accepted: %#v, %v", preview.Signature, err)
			}
			handoff := postAssistant(handler, "root", "", "/v1/offline-bundles",
				`{"action":"handoff","bundleId":"`+envelope.Manifest.ID+`","site":"offline-site","confirm":true}`)
			if handoff.Code != http.StatusBadGateway {
				t.Fatalf("invalid evidence handoff = %d %s", handoff.Code, handoff.Body.String())
			}
		})
	}
}
