package controlplane

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/artifacts"
)

// The Bundle envelope format now lives in internal/artifacts. These aliases
// keep the several hundred call sites in this package spelled as they were;
// the definitions moved, the names did not.
type (
	BundleManifest             = artifacts.Manifest
	BundleResource             = artifacts.Resource
	BundleAdvisory             = artifacts.Advisory
	BundleArtifactLayer        = artifacts.ArtifactLayer
	BundleChunk                = artifacts.Chunk
	BundleSecurityReport       = artifacts.SecurityReport
	BundleArtifactSecurity     = artifacts.ArtifactSecurity
	BundleEnvelope             = artifacts.Envelope
	BundleTransferConfirmation = artifacts.TransferConfirmation
)

const (
	BundleSnapshot    = artifacts.KindSnapshot
	BundleIncremental = artifacts.KindIncremental
	BundleCheckpoint  = artifacts.KindCheckpoint

	BundleAdvisoryNoProductReleases     = artifacts.AdvisoryNoProductReleases
	BundleAdvisoryUnimplementedStrategy = artifacts.AdvisoryUnimplementedStrategy
)

// These are var aliases rather than new errors on purpose: errors.Is compares
// identity, so every existing check against these keeps working unchanged.
var (
	ErrDigestMismatch  = artifacts.ErrDigestMismatch
	ErrBundleUntrusted = artifacts.ErrUntrusted
)

func sha256Digest(sum [sha256.Size]byte) string { return artifacts.Digest(sum) }

func bundleManifestIdentity(manifest BundleManifest) (string, error) {
	return artifacts.ManifestIdentity(manifest)
}

// GenerateBundleSigningKey makes a new Bundle signing key.
func GenerateBundleSigningKey() (ed25519.PrivateKey, error) {
	return artifacts.GenerateSigningKey()
}

func bundleSigningKeyID(key ed25519.PublicKey) string { return artifacts.SigningKeyID(key) }

func bundleBaseline(bundles []BundleEnvelope) (map[string]BundleResource, string) {
	return artifacts.Baseline(bundles)
}

func changedBundleResources(resources []BundleResource, baseline map[string]BundleResource) []BundleResource {
	return artifacts.ChangedResources(resources, baseline)
}

func incrementalsSinceCheckpoint(bundles []BundleEnvelope) int {
	return artifacts.IncrementalsSinceCheckpoint(bundles)
}

// verifyEnvelope defers to Artifacts and then restates one class of failure in
// this package's vocabulary. Artifacts calls a malformed envelope
// ErrEnvelopeInvalid, which is the right name inside a package that knows
// nothing about HTTP; out here the same condition has always been ErrConflict,
// and the handlers map that to 409. Wrapping both keeps the status code and the
// specific message, so nothing a caller could observe changes.
func (service *BundleService) verifyEnvelope(envelope BundleEnvelope) error {
	err := artifacts.VerifyEnvelope(envelope, service.trusted)
	if errors.Is(err, artifacts.ErrEnvelopeInvalid) {
		return fmt.Errorf("%w: %w", ErrConflict, err)
	}
	return err
}
