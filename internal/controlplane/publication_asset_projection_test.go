package controlplane

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
)

// Only PutBlob is implemented: the facade must not retain aggregate authority.
type publicationAssetWriteFixture struct {
	contexts       []context.Context
	digests, media []string
	contents       [][]byte
	failAt         int
	failure        error
}

func (store *publicationAssetWriteFixture) PutBlob(ctx context.Context, digest, media string, source io.Reader) (BlobDescriptor, bool, error) {
	contents, err := io.ReadAll(source)
	if err != nil {
		return BlobDescriptor{}, false, err
	}
	store.contexts = append(store.contexts, ctx)
	store.digests = append(store.digests, digest)
	store.media = append(store.media, media)
	store.contents = append(store.contents, contents)
	if store.failAt == len(store.contents) {
		return BlobDescriptor{}, false, store.failure
	}
	return BlobDescriptor{MediaType: "stored/" + media, Digest: "sha256:stored", Size: int64(len(contents))}, true, nil
}

func TestPublicationAssetsUseNarrowWriterAndPreserveOriginalInputs(t *testing.T) {
	store := &publicationAssetWriteFixture{}
	api := &apolloPublicAPI{publicationAssets: store}
	bundle := HelmProductReleaseBundle{DefaultConfig: []byte("message: café\n"), ConfigSchema: []byte(`{"type":"object"}`)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	assets, err := api.storeHelmPublicationAssets(ctx, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.contents, [][]byte{bundle.DefaultConfig, bundle.ConfigSchema}) || !reflect.DeepEqual(store.media, []string{"application/yaml", "application/schema+json"}) || !reflect.DeepEqual(store.digests, []string{"", ""}) {
		t.Fatalf("asset inputs changed: %+v", store)
	}
	for _, seen := range store.contexts {
		if seen != ctx {
			t.Fatal("original context lost")
		}
	}
	if assets.DefaultConfig == nil || assets.ConfigSchema == nil {
		t.Fatalf("missing assets: %+v", assets)
	}
	if assets.DefaultConfig.MediaType != "stored/application/yaml" || assets.ConfigSchema.MediaType != "stored/application/schema+json" || assets.DefaultConfig.Digest != "sha256:stored" || assets.ConfigSchema.Digest != "sha256:stored" || assets.DefaultConfig.Size != int64(len(bundle.DefaultConfig)) || assets.ConfigSchema.Size != int64(len(bundle.ConfigSchema)) {
		t.Fatalf("stored descriptor changed: %+v", assets)
	}
}

func TestPublicationAssetsSkipEmptyInputsAndPropagateWriterFailure(t *testing.T) {
	empty := &publicationAssetWriteFixture{}
	assets, err := (&apolloPublicAPI{publicationAssets: empty}).storeHelmPublicationAssets(t.Context(), HelmProductReleaseBundle{})
	if err != nil || !reflect.DeepEqual(assets, apolloapp.PublicationAssets{}) || len(empty.contents) != 0 {
		t.Fatalf("empty assets wrote: %+v %v", assets, err)
	}
	failure := errors.New("asset writer unavailable")
	for _, failAt := range []int{1, 2} {
		store := &publicationAssetWriteFixture{failAt: failAt, failure: failure}
		got, err := (&apolloPublicAPI{publicationAssets: store}).storeHelmPublicationAssets(t.Context(), HelmProductReleaseBundle{DefaultConfig: []byte("config"), ConfigSchema: []byte("schema")})
		if err != failure || !reflect.DeepEqual(got, apolloapp.PublicationAssets{}) || len(store.contents) != failAt {
			t.Fatalf("failure changed at %d: %+v %v writes=%d", failAt, got, err, len(store.contents))
		}
	}
}
