package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/artifacts/domain"
	"github.com/jonathanagustin/conduit/internal/artifacts/ports"
)

// memoryBlobs is a content-addressed store that behaves the way the port says
// one must: it verifies a non-empty expected digest before the content becomes
// readable. A fake that skipped that would let a reassembly bug pass here and
// fail against every real store.
type memoryBlobs struct {
	contents map[string][]byte
	putErr   error
	puts     []string
}

func newBlobs() *memoryBlobs { return &memoryBlobs{contents: map[string][]byte{}} }

func (blobs *memoryBlobs) PutBlob(
	_ context.Context, expectedDigest, mediaType string, contents io.Reader,
) (domain.BlobDescriptor, bool, error) {
	if blobs.putErr != nil {
		return domain.BlobDescriptor{}, false, blobs.putErr
	}
	body, err := io.ReadAll(contents)
	if err != nil {
		return domain.BlobDescriptor{}, false, err
	}
	digest := domain.Digest(sha256.Sum256(body))
	if expectedDigest != "" && expectedDigest != digest {
		return domain.BlobDescriptor{}, false, domain.ErrDigestMismatch
	}
	blobs.puts = append(blobs.puts, digest)
	_, existed := blobs.contents[digest]
	blobs.contents[digest] = body
	return domain.BlobDescriptor{
		Digest: digest, Size: int64(len(body)), MediaType: domain.NormalizeMediaType(mediaType),
	}, !existed, nil
}

func (blobs *memoryBlobs) OpenBlob(
	_ context.Context, digest string,
) (domain.BlobDescriptor, io.ReadCloser, error) {
	body, present := blobs.contents[digest]
	if !present {
		return domain.BlobDescriptor{}, nil, errors.New("not found")
	}
	return domain.BlobDescriptor{Digest: digest, Size: int64(len(body))},
		io.NopCloser(strings.NewReader(string(body))), nil
}

var _ ports.BlobStore = (*memoryBlobs)(nil)

func chunk(data string) domain.Chunk {
	return domain.Chunk{Digest: domain.Digest(sha256.Sum256([]byte(data))), Data: []byte(data)}
}

func TestEnsureLayersReassemblesChunksUnderTheManifestDigest(t *testing.T) {
	t.Parallel()
	first, second := chunk("hello "), chunk("world")
	layerBytes := append(append([]byte(nil), first.Data...), second.Data...)
	layerDigest := domain.Digest(sha256.Sum256(layerBytes))

	blobs := newBlobs()
	envelope := domain.Envelope{
		Manifest: domain.Manifest{ArtifactLayers: []domain.ArtifactLayer{{
			Digest: layerDigest, Size: int64(len(layerBytes)), MediaType: "application/vnd.oci.image.layer.v1.tar",
			ChunkDigests: []string{first.Digest, second.Digest},
		}}},
		Chunks: []domain.Chunk{first, second},
	}
	if err := NewStaging(blobs).EnsureLayers(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	if string(blobs.contents[layerDigest]) != "hello world" {
		t.Fatalf("layer contents = %q", blobs.contents[layerDigest])
	}
	// Chunk order is the manifest's, not the envelope's: the chunks are indexed
	// by digest and assembled in the order the layer lists them.
	reversed := domain.Envelope{
		Manifest: envelope.Manifest,
		Chunks:   []domain.Chunk{second, first},
	}
	if err := NewStaging(newBlobs()).EnsureLayers(context.Background(), reversed); err != nil {
		t.Fatalf("chunk order was taken from the envelope rather than the layer: %v", err)
	}
}

// A chunk set that does not add up to the digest the manifest claims must fail
// here. The store verifies it, which is why the port requires verification
// before the content becomes visible.
func TestEnsureLayersRefusesChunksThatDoNotMatchTheClaimedDigest(t *testing.T) {
	t.Parallel()
	only := chunk("hello")
	envelope := domain.Envelope{
		Manifest: domain.Manifest{ArtifactLayers: []domain.ArtifactLayer{{
			Digest: domain.Digest(sha256.Sum256([]byte("something else"))), Size: 5,
			ChunkDigests: []string{only.Digest},
		}}},
		Chunks: []domain.Chunk{only},
	}
	if err := NewStaging(newBlobs()).EnsureLayers(context.Background(), envelope); err == nil {
		t.Fatal("a layer whose chunks do not add up to its digest was stored")
	}
}

func TestEnsureLayersRequiresATransferredLayerToAlreadyBePresent(t *testing.T) {
	t.Parallel()
	body := []byte("out of band")
	digest := domain.Digest(sha256.Sum256(body))
	layer := domain.ArtifactLayer{Digest: digest, Size: int64(len(body)), Transferred: true}
	envelope := domain.Envelope{Manifest: domain.Manifest{ArtifactLayers: []domain.ArtifactLayer{layer}}}

	// Absent is its own failure. It is not corruption, and it must not be
	// silently repaired from chunks the Bundle may not even carry.
	err := NewStaging(newBlobs()).EnsureLayers(context.Background(), envelope)
	if !errors.Is(err, domain.ErrLayerMissing) {
		t.Fatalf("an absent transferred layer produced %v", err)
	}

	present := newBlobs()
	present.contents[digest] = body
	if err := NewStaging(present).EnsureLayers(context.Background(), envelope); err != nil {
		t.Fatalf("a present transferred layer was rejected: %v", err)
	}
	// Nothing was written: a transferred layer is checked, never re-uploaded.
	if len(present.puts) != 0 {
		t.Fatalf("a transferred layer was re-stored: %v", present.puts)
	}

	// Same digest, different length. The store found something under the name,
	// but it is not what the manifest describes.
	wrongSize := newBlobs()
	wrongSize.contents[digest] = []byte("shorter")
	if err := NewStaging(wrongSize).EnsureLayers(context.Background(), envelope); !errors.Is(err, domain.ErrDigestMismatch) {
		t.Fatalf("a transferred layer of the wrong size produced %v", err)
	}
}

func TestEnsureLayersSurfacesAStoreFailure(t *testing.T) {
	t.Parallel()
	only := chunk("hello")
	envelope := domain.Envelope{
		Manifest: domain.Manifest{ArtifactLayers: []domain.ArtifactLayer{{
			Digest: only.Digest, Size: int64(len(only.Data)), ChunkDigests: []string{only.Digest},
		}}},
		Chunks: []domain.Chunk{only},
	}
	blobs := newBlobs()
	blobs.putErr = errors.New("store unavailable")
	if err := NewStaging(blobs).EnsureLayers(context.Background(), envelope); err == nil {
		t.Fatal("a store failure was swallowed")
	}
}

func TestEnsureLayersOnAnEnvelopeWithNoArtifactsIsANoOp(t *testing.T) {
	t.Parallel()
	blobs := newBlobs()
	if err := NewStaging(blobs).EnsureLayers(context.Background(), domain.Envelope{}); err != nil {
		t.Fatal(err)
	}
	if len(blobs.puts) != 0 {
		t.Fatalf("an envelope carrying no artifacts wrote %v", blobs.puts)
	}
}
