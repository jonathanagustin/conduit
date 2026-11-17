package controlplane

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	artifacts "github.com/jonathanagustin/conduit/internal/artifacts/domain"
)

// The rehearsal behind docs/operations/key-rotation.md, "Bundle signing key".
//
// A consuming Hub trusts its own signing key and whatever
// BUNDLE_TRUSTED_KEYS_FILE lists. Rotation is therefore three states of that
// file on the consumer, and this drives a real signed Bundle through each: the
// trust list is written in the file's own format and read with the parser
// cmd/platform-delivery reads it with.

func signedBundle(t *testing.T, key ed25519.PrivateKey) BundleEnvelope {
	t.Helper()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	source, err := NewBundleService(repository, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "rotation-export", DisplayName: "Rotation export", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	built, err := source.Build(ctx, "rotation-export")
	if err != nil || len(built) != 1 {
		t.Fatalf("build = %d bundles, %v", len(built), err)
	}
	return built[0]
}

// trustFile renders BUNDLE_TRUSTED_KEYS_FILE for these signers and reads it
// back the way the Hub does.
func trustFile(t *testing.T, signers ...ed25519.PrivateKey) map[string]ed25519.PublicKey {
	t.Helper()
	entries := map[string]string{}
	for _, signer := range signers {
		public := signer.Public().(ed25519.PublicKey)
		entries[artifacts.SigningKeyID(public)] = base64.RawStdEncoding.EncodeToString(public)
	}
	contents, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := artifacts.ParseTrustedKeys(contents)
	if err != nil {
		t.Fatal(err)
	}
	return trusted
}

func TestBundleSigningKeyRotationWithOverlapAndRetirement(t *testing.T) {
	oldKey, newKey := mustBundleKey(t), mustBundleKey(t)
	fromOld, fromNew := signedBundle(t, oldKey), signedBundle(t, newKey)
	if fromOld.SigningKeyID == fromNew.SigningKeyID {
		t.Fatal("two signers share a key identifier")
	}
	consumer := func(trusted map[string]ed25519.PublicKey) func(BundleEnvelope) bool {
		// The consumer's own signing key is a third key: it is always trusted,
		// and it is neither of the two being rotated.
		service, err := NewBundleService(NewMemoryRepository(), mustBundleKey(t), trusted)
		if err != nil {
			t.Fatal(err)
		}
		return func(envelope BundleEnvelope) bool { return service.signatureEvidence(envelope).Verified }
	}

	// Before: the consumer trusts the current signer only.
	verifies := consumer(trustFile(t, oldKey))
	if !verifies(fromOld) || verifies(fromNew) {
		t.Fatalf("before rotation: old=%t new=%t, want old only", verifies(fromOld), verifies(fromNew))
	}
	// Overlap: the successor's public key is published BEFORE the source signs
	// with it. Bundles from either signer verify, so the source can switch and
	// Bundles already in transit still land.
	verifies = consumer(trustFile(t, oldKey, newKey))
	if !verifies(fromOld) || !verifies(fromNew) {
		t.Fatalf("during overlap: old=%t new=%t, want both", verifies(fromOld), verifies(fromNew))
	}
	// Retirement: the predecessor is removed. Anything it signed -- including a
	// Bundle built before the switch and imported late -- is refused.
	verifies = consumer(trustFile(t, newKey))
	if verifies(fromOld) || !verifies(fromNew) {
		t.Fatalf("after retirement: old=%t new=%t, want new only", verifies(fromOld), verifies(fromNew))
	}
	// A consumer with no trust file trusts only itself.
	verifies = consumer(nil)
	if verifies(fromOld) || verifies(fromNew) {
		t.Fatal("a consumer with no trust list verified another Hub's Bundle")
	}
}

// The identifier in the trust file is derived from the key. An entry filed
// under any other name stops the Hub starting, rather than being trusted under
// a label that does not match what it is.
func TestATrustListEntryFiledUnderTheWrongIdentifierIsRefusedAtStart(t *testing.T) {
	public := mustBundleKey(t).Public().(ed25519.PublicKey)
	contents, err := json.Marshal(map[string]string{"key_0000000000000000": base64.RawStdEncoding.EncodeToString(public)})
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := artifacts.ParseTrustedKeys(contents)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBundleService(NewMemoryRepository(), mustBundleKey(t), trusted); err == nil ||
		!strings.Contains(err.Error(), "invalid trusted Bundle signing key") {
		t.Fatalf("a mislabelled trusted key was accepted: %v", err)
	}
	if _, err := artifacts.ParseTrustedKeys([]byte(`{"key_x":"not-a-key"}`)); err == nil {
		t.Fatal("a trust file entry that is not an Ed25519 public key was accepted")
	}
}
