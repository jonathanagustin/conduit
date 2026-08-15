package controlplane

import (
	"context"
	"testing"
)

// Bundle import changes must carry the origin metadata the published import
// predicate reads, otherwise a policy written upstream can never recognize them.
func TestBundleImportChangesCarryImportOriginMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	envelope := BundleEnvelope{Manifest: BundleManifest{
		ID: "bnd_import_1",
		Resources: []BundleResource{
			{Type: "ENVIRONMENT", ID: "dev-use2", Digest: digest},
		},
	}}
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "svc-import", ServiceAccount: true})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := service.bundleImportChanges(ctx, envelope, "dev-use2", author)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	metadata := changes[0].Metadata
	if metadata[MetadataChangeSource] != ChangeSourceEnvironmentImport || metadata[MetadataBundleID] != "bnd_import_1" {
		t.Fatalf("metadata = %#v", metadata)
	}
}
