// Package application makes a Bundle's content real: it takes an envelope whose
// signature has already been checked and ensures every artifact layer it
// describes is present in the store, byte for byte.
//
// It is the only place the two halves meet. What a layer is and how it is
// addressed is the domain's; where bytes live is the port's. Reassembling the
// chunks and deciding that an absent layer is a different failure from a
// corrupt one is what is left, and it is all that is here.
package application

import (
	"bytes"
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/artifacts/domain"
	"github.com/jonathanagustin/conduit/internal/artifacts/ports"
)

// Staging materialises Bundle artifact layers into a blob store.
type Staging struct {
	blobs ports.BlobStore
}

// NewStaging binds staging to a store.
func NewStaging(blobs ports.BlobStore) *Staging {
	return &Staging{blobs: blobs}
}

// EnsureLayers makes every artifact layer the envelope describes present.
//
// A layer marked transferred was moved out of band and must already be here;
// this checks that it is, and that it is the size the manifest claims. It is
// deliberately not re-fetched or reassembled from chunks: an out-of-band
// transfer that did not arrive is a fact worth surfacing, not one to paper over
// with a second copy the Bundle may not even carry.
//
// Every other layer is reassembled from the envelope's chunks and written under
// the digest the manifest gives. The store verifies that digest, so a chunk set
// that does not add up to the claimed layer fails here rather than becoming
// readable under a name it does not match.
func (staging *Staging) EnsureLayers(ctx context.Context, envelope domain.Envelope) error {
	chunks := make(map[string][]byte, len(envelope.Chunks))
	for _, chunk := range envelope.Chunks {
		chunks[chunk.Digest] = chunk.Data
	}
	for _, layer := range envelope.Manifest.ArtifactLayers {
		if layer.Transferred {
			descriptor, reader, err := staging.blobs.OpenBlob(ctx, layer.Digest)
			if err != nil {
				return fmt.Errorf("%w: target lacks confirmed transferred layer %s", domain.ErrLayerMissing, layer.Digest)
			}
			_ = reader.Close()
			if descriptor.Size != layer.Size {
				return fmt.Errorf("%w: transferred layer %s size differs", domain.ErrDigestMismatch, layer.Digest)
			}
			continue
		}
		var contents bytes.Buffer
		for _, digest := range layer.ChunkDigests {
			contents.Write(chunks[digest])
		}
		if _, _, err := staging.blobs.PutBlob(ctx, layer.Digest, layer.MediaType, bytes.NewReader(contents.Bytes())); err != nil {
			return fmt.Errorf("store Bundle artifact layer %s: %w", layer.Digest, err)
		}
	}
	return nil
}
