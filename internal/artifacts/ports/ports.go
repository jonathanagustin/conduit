// Package ports declares what Artifacts needs from the world in order to make a
// Bundle's content real, and nothing more.
//
// There is one port: somewhere to keep immutable bytes, addressed by digest. It
// is not a repository. Artifacts does not want transactions, queries or a
// lifecycle — it wants to put bytes under a name it computed and get them back
// under the same name, and naming exactly that is what keeps a file store, an
// object store and a database column interchangeable underneath.
package ports

import (
	"context"
	"io"

	"github.com/jonathanagustin/conduit/internal/artifacts/domain"
)

// BlobStore keeps immutable bytes by digest.
//
// expectedDigest may be empty, which is a server-computed upload: the store
// assigns the identity. When it is not empty the implementation must verify it
// before making the content visible — verifying afterwards means there is a
// moment when corrupt bytes are readable under a good name.
//
// A repeated upload of content already present is not an error. Content
// addressing makes the second write either identical or a digest mismatch, and
// there is no third case for a caller to handle.
type BlobStore interface {
	PutBlob(ctx context.Context, expectedDigest, mediaType string, contents io.Reader) (domain.BlobDescriptor, bool, error)
	OpenBlob(ctx context.Context, digest string) (domain.BlobDescriptor, io.ReadCloser, error)
}
