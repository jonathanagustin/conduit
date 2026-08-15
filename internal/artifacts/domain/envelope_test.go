package domain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func signed(t *testing.T, resources []Resource, layers []ArtifactLayer, chunks []Chunk) (Envelope, map[string]ed25519.PublicKey) {
	t.Helper()
	key, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Sign(
		key,
		Origin{PipelineID: "pipe", PipelineRevision: 3, Sequence: 1, Kind: KindSnapshot, ResourceTypes: []string{"RELEASE"}},
		time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
		resources, layers, chunks, SecurityReport{Outcome: "PASS"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	public := key.Public().(ed25519.PublicKey)
	return envelope, map[string]ed25519.PublicKey{SigningKeyID(public): public}
}

func resource(id string, payload string) Resource {
	r := Resource{Type: "RELEASE", ID: id, Payload: json.RawMessage(payload)}
	r.Digest = ResourceDigest(r)
	return r
}

func TestSignedEnvelopeVerifies(t *testing.T) {
	t.Parallel()
	envelope, trusted := signed(t, []Resource{resource("a", `{"v":1}`)}, nil, nil)
	if err := VerifyEnvelope(envelope, trusted); err != nil {
		t.Fatalf("a freshly signed envelope must verify: %v", err)
	}
	if envelope.Manifest.ID == "" || envelope.Manifest.APIVersion != APIVersion {
		t.Fatalf("manifest is not self-describing: %#v", envelope.Manifest)
	}
}

// The identity deliberately excludes CreatedAt, so rebuilding the same content
// later yields the same Bundle rather than a new one. If that stops being true,
// a retried build looks like a distinct Bundle and lineage forks silently.
func TestIdentityIgnoresBuildTime(t *testing.T) {
	t.Parallel()
	first, _ := signed(t, []Resource{resource("a", `{"v":1}`)}, nil, nil)
	key, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(
		key,
		Origin{PipelineID: "pipe", PipelineRevision: 3, Sequence: 1, Kind: KindSnapshot, ResourceTypes: []string{"RELEASE"}},
		time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC),
		[]Resource{resource("a", `{"v":1}`)}, nil, nil, SecurityReport{Outcome: "PASS"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Manifest.ID != second.Manifest.ID {
		t.Fatalf("same content built at a different time changed identity: %s vs %s", first.Manifest.ID, second.Manifest.ID)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		break_  func(*Envelope)
		wantErr error
	}{
		{"altered payload after signing", func(e *Envelope) {
			e.Manifest.Resources[0].Payload = json.RawMessage(`{"v":666}`)
		}, ErrDigestMismatch},
		{"restated manifest digest", func(e *Envelope) {
			e.ManifestDigest = Digest(sha256.Sum256([]byte("not the manifest")))
		}, ErrDigestMismatch},
		{"forged signature", func(e *Envelope) { e.Signature = "AAAA" }, ErrUntrusted},
		{"unknown signing key", func(e *Envelope) { e.SigningKeyID = "key_deadbeefdeadbeef" }, ErrUntrusted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			envelope, trusted := signed(t, []Resource{resource("a", `{"v":1}`)}, nil, nil)
			testCase.break_(&envelope)
			err := VerifyEnvelope(envelope, trusted)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("want %v, got %v", testCase.wantErr, err)
			}
		})
	}
}

// A malformed envelope is a different thing from a corrupt one, and the
// distinction is what lets a caller answer 409 rather than 422.
func TestMalformedEnvelopeIsNotADigestFailure(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		break_ func(*Envelope, ed25519.PrivateKey) Envelope
	}{
		{"unknown kind", func(e *Envelope, key ed25519.PrivateKey) Envelope {
			out, err := Sign(key, Origin{PipelineID: "p", Kind: "SIDEWAYS"}, time.Now(), nil, nil, nil, SecurityReport{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			key, err := GenerateSigningKey()
			if err != nil {
				t.Fatal(err)
			}
			public := key.Public().(ed25519.PublicKey)
			envelope := testCase.break_(nil, key)
			err = VerifyEnvelope(envelope, map[string]ed25519.PublicKey{SigningKeyID(public): public})
			if !errors.Is(err, ErrEnvelopeInvalid) {
				t.Fatalf("want ErrEnvelopeInvalid, got %v", err)
			}
			if errors.Is(err, ErrDigestMismatch) {
				t.Fatal("a well-formed-but-unsupported envelope must not read as corruption")
			}
		})
	}
}

func TestDuplicateResourceIsRejected(t *testing.T) {
	t.Parallel()
	envelope, trusted := signed(t, []Resource{resource("a", `{"v":1}`), resource("a", `{"v":1}`)}, nil, nil)
	if err := VerifyEnvelope(envelope, trusted); !errors.Is(err, ErrEnvelopeInvalid) {
		t.Fatalf("want ErrEnvelopeInvalid for a resource listed twice, got %v", err)
	}
}

func TestChunksMustReconstructTheirLayer(t *testing.T) {
	t.Parallel()
	data := []byte("layer contents")
	sum := sha256.Sum256(data)
	chunk := Chunk{Digest: Digest(sum), Size: len(data), Data: data}
	layer := ArtifactLayer{Digest: Digest(sum), MediaType: "application/octet-stream", Size: int64(len(data)), ChunkDigests: []string{chunk.Digest}}

	envelope, trusted := signed(t, nil, []ArtifactLayer{layer}, []Chunk{chunk})
	if err := VerifyEnvelope(envelope, trusted); err != nil {
		t.Fatalf("chunks that reconstruct their layer must verify: %v", err)
	}

	short, trustedShort := signed(t, nil, []ArtifactLayer{layer}, []Chunk{{Digest: chunk.Digest, Size: 3, Data: []byte("lay")}})
	if err := VerifyEnvelope(short, trustedShort); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("want ErrDigestMismatch for a truncated chunk, got %v", err)
	}
}

// A transferred layer carries no chunks by design. Verifying it as if it should
// would reject every incremental that reuses a layer the far end already holds.
func TestTransferredLayerNeedsNoChunks(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("already there"))
	layer := ArtifactLayer{Digest: Digest(sum), MediaType: "application/octet-stream", Size: 13, Transferred: true}
	envelope, trusted := signed(t, nil, []ArtifactLayer{layer}, nil)
	if err := VerifyEnvelope(envelope, trusted); err != nil {
		t.Fatalf("a transferred layer must verify without its bytes: %v", err)
	}
}

// Two deletions of different resources must not share a digest, or a tampered
// tombstone would be indistinguishable from a genuine one.
func TestDeletionDigestsAreDistinct(t *testing.T) {
	t.Parallel()
	first := ResourceDigest(Resource{Type: "RELEASE", ID: "a", Deleted: true})
	second := ResourceDigest(Resource{Type: "RELEASE", ID: "b", Deleted: true})
	if first == second {
		t.Fatalf("deleting different resources produced the same digest: %s", first)
	}
	if first == ResourceDigest(Resource{Type: "PRODUCT", ID: "a", Deleted: true}) {
		t.Fatal("deleting the same id in different types produced the same digest")
	}
}
