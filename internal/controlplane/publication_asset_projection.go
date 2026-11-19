package controlplane

import (
	"context"
	"io"
)

// publicationAssetWriter is the only stored repository capability needed to
// retain publication assets. It grants no Plan, governance or execution access.
type publicationAssetWriter interface {
	PutBlob(context.Context, string, string, io.Reader) (BlobDescriptor, bool, error)
}
