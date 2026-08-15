// Package domain owns the Bundle envelope and the content identity everything
// around it is addressed by: the format a Hub signs, the format another Hub
// verifies, and the rules that decide whether what arrived is what was sent.
//
// It knows nothing about pipelines, plans, releases, or environments. A Bundle
// carries resources of some type with some payload, and this package's whole
// concern is that the bytes are intact, the signature is genuine, and the
// manifest describes itself honestly. What those resources mean is somebody
// else's question.
//
// That is why Sign takes the few manifest fields it needs rather than the
// pipeline they come from: the caller knows about pipelines, and keeping that
// knowledge on the caller's side is what lets this package import nothing.
//
// Storing the bytes is not here either. Where a layer's bytes actually live is
// a BlobStore's business, behind the port next door; what this package settles
// is which bytes they have to be.
package domain

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// APIVersion is the only envelope version this package reads or writes.
const APIVersion = "platform.delivery/bundle/v1alpha1"

// Bundle kinds. A snapshot stands alone; an incremental is a diff against the
// bundle it depends on; a checkpoint collapses a chain back into a full state.
const (
	KindSnapshot    = "SNAPSHOT"
	KindIncremental = "INCREMENTAL"
	KindCheckpoint  = "CHECKPOINT"
)

// SourceFormatNote travels inside every manifest. It is a statement about
// provenance, not decoration: the format is derived from public Apollo
// semantics and is not a claim to be Palantir's wire format.
const SourceFormatNote = "Platform-native format derived from public Apollo semantics; not a Palantir wire-format claim."

var (
	// ErrDigestMismatch means the content does not hash to what its
	// descriptor says. Something was altered or truncated in transit.
	ErrDigestMismatch = errors.New("artifact digest mismatch")
	// ErrUntrusted means the signature is absent, malformed, or made by a key
	// this Hub has not been told to trust. The bytes may be perfectly intact.
	ErrUntrusted = errors.New("untrusted Bundle")
	// ErrEnvelopeInvalid means the envelope is internally inconsistent —
	// a version this package cannot read, a kind it does not know, a resource
	// listed twice. Nothing is corrupt; the envelope is simply not well formed.
	ErrEnvelopeInvalid = errors.New("invalid Bundle envelope")
)

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// Resource is one item a Bundle carries. Payload is opaque here on purpose:
// this package hashes it and never looks inside.
type Resource struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Digest  string          `json:"digest"`
	Deleted bool            `json:"deleted,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ArtifactLayer describes a blob the Bundle carries or expects to already be
// present. Transferred means the far end confirmed it holds the layer, so the
// chunks for it are omitted rather than sent again.
type ArtifactLayer struct {
	Digest       string   `json:"digest"`
	MediaType    string   `json:"mediaType"`
	Size         int64    `json:"size"`
	ChunkDigests []string `json:"chunkDigests,omitempty"`
	Transferred  bool     `json:"transferred,omitempty"`
}

// Chunk is a piece of a layer's bytes, addressed by its own digest.
type Chunk struct {
	Digest string `json:"digest"`
	Size   int    `json:"size"`
	Data   []byte `json:"data"`
}

// ArtifactSecurity is one artifact's scan result at the time the Bundle was
// built. It is a record of what was known then, not a live assessment.
type ArtifactSecurity struct {
	Release             string    `json:"release"`
	ArtifactDigest      string    `json:"artifactDigest"`
	VulnerabilityStatus string    `json:"vulnerabilityStatus"`
	VirusStatus         string    `json:"virusStatus"`
	AssessedAt          time.Time `json:"assessedAt,omitempty"`
}

// SecurityReport is the scan outcome for everything the Bundle carries.
type SecurityReport struct {
	GeneratedAt         time.Time          `json:"generatedAt"`
	Outcome             string             `json:"outcome"`
	ArtifactAssessments []ArtifactSecurity `json:"artifactAssessments,omitempty"`
}

// Advisory records something a Bundle build did not do, on a build that
// otherwise succeeded. A build therefore has three outcomes rather than two:
// it succeeded, it succeeded with advisories, or it failed. Kind is a closed
// set so a reader can group and act on advisories; Message alone is prose, and
// prose has to be parsed back apart to be useful.
type Advisory struct {
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	Subjects []string `json:"subjects,omitempty"`
}

// AdvisoryNoProductReleases reports Products a selection rule named that hold
// no unrecalled Release on the Release Channels it asked for. The Bundle can
// still be transferred, but it does not contain those Products -- and without
// this the omission is indistinguishable from a clean build.
const AdvisoryNoProductReleases = "NO_PRODUCT_RELEASES"

// AdvisoryUnimplementedStrategy reports a selection rule whose Release strategy
// this Hub accepts but does not implement. MINIMAL is validated and then falls
// through to the latest-per-channel path, so the Bundle holds the newest
// Release and none of the intermediate ones a stepwise upgrade needs. Without
// this the build is indistinguishable from one that honoured the request, and
// the omission is only discovered at the far end, where the upgrade cannot
// complete.
//
// This reports the gap; it does not close it. What the Bundle contains is
// unchanged.
const AdvisoryUnimplementedStrategy = "UNIMPLEMENTED_RELEASE_STRATEGY"

// Manifest is what gets signed.
type Manifest struct {
	APIVersion         string          `json:"apiVersion"`
	ID                 string          `json:"id"`
	PipelineID         string          `json:"pipelineId"`
	PipelineRevision   uint64          `json:"pipelineRevision"`
	Standalone         bool            `json:"standalone,omitempty"`
	LineageIndependent bool            `json:"lineageIndependent,omitempty"`
	Sequence           uint64          `json:"sequence"`
	Kind               string          `json:"kind"`
	DependsOn          string          `json:"dependsOn,omitempty"`
	CheckpointOf       string          `json:"checkpointOf,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	Resources          []Resource      `json:"resources"`
	SecurityReport     SecurityReport  `json:"securityReport"`
	ResourceTypes      []string        `json:"resourceTypes"`
	TargetHub          string          `json:"targetHub,omitempty"`
	ArtifactLayers     []ArtifactLayer `json:"artifactLayers,omitempty"`
	SourceFormatNote   string          `json:"sourceFormatNote"`
	Advisories         []Advisory      `json:"advisories,omitempty"`
}

// Envelope is a signed Manifest and the bytes it describes.
type Envelope struct {
	Manifest       Manifest `json:"manifest"`
	ManifestDigest string   `json:"manifestDigest"`
	SigningKeyID   string   `json:"signingKeyId"`
	Signature      string   `json:"signature"`
	Chunks         []Chunk  `json:"chunks,omitempty"`
}

// TransferConfirmation is the far end's receipt: it holds these layers, so a
// later Bundle may reference them instead of sending them again. Rescinding it
// withdraws that promise.
type TransferConfirmation struct {
	ID           string    `json:"id"`
	PipelineID   string    `json:"pipelineId"`
	TargetHub    string    `json:"targetHub"`
	BundleID     string    `json:"bundleId"`
	LayerDigests []string  `json:"layerDigests"`
	ConfirmedAt  time.Time `json:"confirmedAt"`
	RescindedAt  time.Time `json:"rescindedAt,omitempty"`
	Revision     uint64    `json:"revision"`
}

// Digest renders a raw SHA-256 sum in the form every descriptor here uses.
func Digest(sum [sha256.Size]byte) string {
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ResourceDigest hashes a resource. A deletion has no payload to hash, so it
// hashes its own identity instead — otherwise every deletion of every resource
// would share one digest and a tampered deletion would be undetectable.
func ResourceDigest(resource Resource) string {
	if resource.Deleted {
		digest := sha256.Sum256([]byte("DELETE\x00" + resource.Type + "\x00" + resource.ID))
		return Digest(digest)
	}
	digest := sha256.Sum256(resource.Payload)
	return Digest(digest)
}

// ManifestIdentity derives a Bundle's ID from the fields that decide what it
// *is*, deliberately excluding CreatedAt and the security report. Two builds of
// the same content therefore carry the same ID, which is what makes a retried
// build recognisable as the same Bundle rather than a new one.
func ManifestIdentity(manifest Manifest) (string, error) {
	identityJSON, err := json.Marshal(struct {
		PipelineID         string          `json:"pipelineId"`
		PipelineRevision   uint64          `json:"pipelineRevision"`
		Standalone         bool            `json:"standalone,omitempty"`
		LineageIndependent bool            `json:"lineageIndependent,omitempty"`
		Sequence           uint64          `json:"sequence"`
		Kind               string          `json:"kind"`
		Resources          []Resource      `json:"resources"`
		TargetHub          string          `json:"targetHub,omitempty"`
		Layers             []ArtifactLayer `json:"artifactLayers,omitempty"`
	}{
		manifest.PipelineID, manifest.PipelineRevision, manifest.Standalone,
		manifest.LineageIndependent, manifest.Sequence, manifest.Kind,
		manifest.Resources, manifest.TargetHub, manifest.ArtifactLayers,
	})
	if err != nil {
		return "", err
	}
	identity := sha256.Sum256(identityJSON)
	return "bnd_" + hex.EncodeToString(identity[:16]), nil
}

// GenerateSigningKey makes a new Bundle signing key.
func GenerateSigningKey() (ed25519.PrivateKey, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	return privateKey, err
}

// SigningKeyID names a public key by its own hash, so the two cannot disagree.
func SigningKeyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return "key_" + hex.EncodeToString(sum[:8])
}

// Origin is the part of a Manifest that comes from the caller's world rather
// than from this package. Taking these few fields instead of the pipeline they
// came from is what keeps Artifacts free of a dependency on Delivery.
type Origin struct {
	PipelineID         string
	PipelineRevision   uint64
	Sequence           uint64
	Kind               string
	DependsOn          string
	CheckpointOf       string
	TargetHub          string
	Standalone         bool
	LineageIndependent bool
	ResourceTypes      []string
}

// Sign assembles a Manifest, derives its identity, and signs it.
func Sign(
	key ed25519.PrivateKey,
	origin Origin,
	createdAt time.Time,
	resources []Resource,
	layers []ArtifactLayer,
	chunks []Chunk,
	security SecurityReport,
	advisories []Advisory,
) (Envelope, error) {
	manifest := Manifest{
		APIVersion:         APIVersion,
		PipelineID:         origin.PipelineID,
		PipelineRevision:   origin.PipelineRevision,
		Standalone:         origin.Standalone,
		LineageIndependent: origin.LineageIndependent,
		Sequence:           origin.Sequence,
		Kind:               origin.Kind,
		DependsOn:          origin.DependsOn,
		CheckpointOf:       origin.CheckpointOf,
		CreatedAt:          createdAt.UTC(),
		Resources:          resources,
		SecurityReport:     security,
		TargetHub:          origin.TargetHub,
		ArtifactLayers:     layers,
		ResourceTypes:      append([]string(nil), origin.ResourceTypes...),
		SourceFormatNote:   SourceFormatNote,
		Advisories:         advisories,
	}
	id, err := ManifestIdentity(manifest)
	if err != nil {
		return Envelope{}, err
	}
	manifest.ID = id
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return Envelope{}, err
	}
	digest := sha256.Sum256(manifestJSON)
	publicKey := key.Public().(ed25519.PublicKey)
	return Envelope{
		Manifest:       manifest,
		ManifestDigest: Digest(digest),
		SigningKeyID:   SigningKeyID(publicKey),
		Signature:      base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, digest[:])),
		Chunks:         chunks,
	}, nil
}

// VerifyEnvelope decides whether an envelope may be believed. It checks, in
// order: that the manifest hashes to its stated digest, that the signature was
// made by a trusted key over that digest, that the manifest is one this package
// understands, that the ID matches the identity fields, that no resource is
// listed twice or carries a payload it claims to have deleted, and finally that
// every chunk and layer matches its descriptor.
//
// The order matters. Signature first means a manifest that was tampered with is
// rejected before anything inside it is trusted enough to inspect.
func VerifyEnvelope(envelope Envelope, trusted map[string]ed25519.PublicKey) error {
	manifestJSON, err := json.Marshal(envelope.Manifest)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(manifestJSON)
	if envelope.ManifestDigest != Digest(digest) {
		return fmt.Errorf("%w: Bundle manifest digest does not match contents", ErrDigestMismatch)
	}
	key, isTrusted := trusted[envelope.SigningKeyID]
	if !isTrusted {
		return fmt.Errorf("%w: Bundle signing key %s is not trusted", ErrUntrusted, envelope.SigningKeyID)
	}
	signature, err := base64.RawStdEncoding.DecodeString(envelope.Signature)
	if err != nil || !ed25519.Verify(key, digest[:], signature) {
		return fmt.Errorf("%w: Bundle signature is invalid", ErrUntrusted)
	}
	if envelope.Manifest.APIVersion != APIVersion {
		return fmt.Errorf("%w: unsupported Bundle API version", ErrEnvelopeInvalid)
	}
	if envelope.Manifest.Kind != KindSnapshot && envelope.Manifest.Kind != KindIncremental &&
		envelope.Manifest.Kind != KindCheckpoint {
		return fmt.Errorf("%w: unsupported Bundle kind", ErrEnvelopeInvalid)
	}
	expectedID, err := ManifestIdentity(envelope.Manifest)
	if err != nil {
		return err
	}
	if envelope.Manifest.ID != expectedID {
		return fmt.Errorf("%w: Bundle id does not match its identity fields", ErrDigestMismatch)
	}
	seen := make(map[string]struct{}, len(envelope.Manifest.Resources))
	for _, resource := range envelope.Manifest.Resources {
		key := resource.Type + ":" + resource.ID
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate Bundle resource %s", ErrEnvelopeInvalid, key)
		}
		seen[key] = struct{}{}
		if resource.Deleted && len(resource.Payload) != 0 {
			return fmt.Errorf("%w: deleted Bundle resource %s has a payload", ErrEnvelopeInvalid, key)
		}
		if resource.Digest != ResourceDigest(resource) {
			return fmt.Errorf("%w: Bundle resource %s digest mismatch", ErrDigestMismatch, key)
		}
	}
	return VerifyChunks(envelope)
}

// VerifyChunks checks that the carried bytes reconstruct the layers the
// manifest describes. A layer marked transferred carries no chunks by design
// and is skipped here; whether the far end really holds it is a question for
// whoever owns the blob store, not for this package.
func VerifyChunks(envelope Envelope) error {
	chunks := make(map[string]Chunk, len(envelope.Chunks))
	for _, chunk := range envelope.Chunks {
		if _, exists := chunks[chunk.Digest]; exists {
			return fmt.Errorf("%w: duplicate Bundle chunk %s", ErrEnvelopeInvalid, chunk.Digest)
		}
		sum := sha256.Sum256(chunk.Data)
		if chunk.Size != len(chunk.Data) || chunk.Digest != Digest(sum) {
			return fmt.Errorf("%w: Bundle chunk %s does not match its descriptor", ErrDigestMismatch, chunk.Digest)
		}
		chunks[chunk.Digest] = chunk
	}
	layers := make(map[string]struct{}, len(envelope.Manifest.ArtifactLayers))
	for _, layer := range envelope.Manifest.ArtifactLayers {
		if _, exists := layers[layer.Digest]; exists {
			return fmt.Errorf("%w: duplicate Bundle artifact layer %s", ErrEnvelopeInvalid, layer.Digest)
		}
		layers[layer.Digest] = struct{}{}
		if layer.Size < 0 || !digestPattern.MatchString(layer.Digest) {
			return fmt.Errorf("%w: invalid Bundle artifact layer descriptor", ErrDigestMismatch)
		}
		if layer.Transferred {
			continue
		}
		var contents bytes.Buffer
		for _, digest := range layer.ChunkDigests {
			chunk, exists := chunks[digest]
			if !exists {
				return fmt.Errorf("%w: Bundle artifact layer %s is missing chunk %s", ErrDigestMismatch, layer.Digest, digest)
			}
			contents.Write(chunk.Data)
		}
		sum := sha256.Sum256(contents.Bytes())
		if int64(contents.Len()) != layer.Size || Digest(sum) != layer.Digest {
			return fmt.Errorf("%w: Bundle artifact layer %s does not match its descriptor", ErrDigestMismatch, layer.Digest)
		}
	}
	return nil
}
