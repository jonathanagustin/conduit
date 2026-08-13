package foundryinterop

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestRecoveredApolloStructuralInventory(t *testing.T) {
	want := []string{
		"ApolloAgentEntityInfo.java", "ApolloAgentInfoUpdate.java", "ApolloAgentSecretType.java",
		"ApolloArtifactUri.java", "ApolloBlockSetPublishingStatus.java", "ApolloBlockSetPublishingStatusfailure.java",
		"ApolloBlockSetPublishingStatusinProgress.java", "ApolloBlockSetPublishingStatussuccess.java", "ApolloCrossStackManifest.java",
		"ApolloEntityId.java", "ApolloEnvironment.java", "ApolloEnvironmentId.java", "ApolloFoundrySpaceIdentifier.java",
		"ApolloGranularConfig.java", "ApolloHostUri.java", "ApolloManagedInstallationConfigurationV0.java", "ApolloPlanId.java",
		"ApolloPlanRid.java", "ApolloPlanTaskRid.java", "ApolloPublishError.java", "ApolloSecretAndKeyName.java",
		"ApolloSecretName.java", "ApolloSpaceId.java", "ApolloSpaceIdentifiers.java", "ApolloSpaceToPublishingStatus.java",
		"ApolloStackLevelConfig.java", "ApolloStackManagementConfig.java", "ApolloStackManagementConfiggranularConfig.java",
		"ApolloStackManagementConfigstackLevelConfig.java", "ApolloUniversalInstallationConfigurationV0.java",
		"BatchGetApolloBlockSetVersionPublishingStatusResponse.java", "BlockSetPublishingJobSuccess.java",
		"CreateInstallationForBlockSetRequest.java", "FailedInstallationCreationRequest.java", "FoundryProductsDownloaderStore.java",
		"ManageExistingInstallationRequest.java", "ManagedInstallation.java", "ReleaseCreationErrorapolloPublishError.java",
		"SecretResolvedEdgePipelineMagritteSourceInputShape.java", "SecretResolvedMarkingsInputShape.java",
		"SecretResolvedNamedCredentialInputShape.java", "UpdateInstallationForBlockSetRequest.java",
	}
	if len(StructuralTypeInventory) != 42 {
		t.Fatalf("inventory has %d entries, want 42", len(StructuralTypeInventory))
	}
	got := append([]string(nil), StructuralTypeInventory...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("structural inventory mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestRecoveredApolloTaggedStructuresMarshalExactly(t *testing.T) {
	defaultEnabled := true
	forceDefault := false
	access := ManagedStoreViewEverything
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "publishing success",
			value: ApolloBlockSetPublishingStatusSuccess{
				Type: "success", Success: BlockSetPublishingJobSuccess{ArtifactURI: "ri.apollo.main.artifact.1"},
			},
			want: `{"type":"success","success":{"artifactUri":"ri.apollo.main.artifact.1"}}`,
		},
		{
			name:  "publishing in progress",
			value: ApolloBlockSetPublishingStatusInProgress{Type: "inProgress", InProgress: BlockSetPublishingJobInProgress{}},
			want:  `{"type":"inProgress","inProgress":{}}`,
		},
		{
			name:  "publishing failure",
			value: ApolloBlockSetPublishingStatusFailure{Type: "failure", Failure: BlockSetPublishingJobFailure{Error: json.RawMessage(`{"message":"rejected"}`)}},
			want:  `{"type":"failure","failure":{"error":{"message":"rejected"}}}`,
		},
		{
			name: "granular stack configuration",
			value: ApolloStackManagementConfigGranularConfig{Type: "granularConfig", GranularConfig: ApolloGranularConfig{
				DisallowedEnrollmentRIDs: []string{"ri.enrollment.blocked"}, DisallowedOrganizationRIDs: []string{},
				EnrollmentsRIDs: []string{"ri.enrollment.allowed"}, GroupIDs: []string{"group"}, OrganizationRIDs: []string{"ri.organization.allowed"},
			}},
			want: `{"type":"granularConfig","granularConfig":{"disallowedEnrollmentRids":["ri.enrollment.blocked"],"disallowedOrganizationRids":[],"enrollmentsRids":["ri.enrollment.allowed"],"groupIds":["group"],"organizationRids":["ri.organization.allowed"]}}`,
		},
		{
			name: "stack-level configuration",
			value: ApolloStackManagementConfigStackLevelConfig{Type: "stackLevelConfig", StackLevelConfig: ApolloStackLevelConfig{
				DefaultEnabled: &defaultEnabled, DefaultStoreAccess: &access, ForceDefault: &forceDefault,
			}},
			want: `{"type":"stackLevelConfig","stackLevelConfig":{"defaultEnabled":true,"defaultStoreAccess":"VIEW_EVERYTHING","forceDefault":false}}`,
		},
		{
			name:  "markings alias",
			value: SecretResolvedMarkingsInputShape(ApolloSecretAndKeyName{SecretKeyName: "markings", SecretName: "secret-markings"}),
			want:  `{"secretKeyName":"markings","secretName":"secret-markings"}`,
		},
		{
			name:  "publish error",
			value: ReleaseCreationErrorApolloPublishError{Type: "apolloPublishError", ApolloPublishError: ApolloPublishError{Reason: "denied"}},
			want:  `{"type":"apolloPublishError","apolloPublishError":{"reason":"denied"}}`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("marshal = %s\nwant    = %s", got, test.want)
			}
		})
	}
}

func TestRecoveredApolloInstallationEnvelopePreservesOpaqueFoundryShapes(t *testing.T) {
	fixture := []byte(`{
		"apolloAgentEntityInfo":{"apolloEnvironmentId":"env","apolloSpaceId":"space","foundrySpaceMavenGroup":"com.example.space","installationEntityId":"entity","secretInputMapping":{}},
		"blockSetId":"block-set","configuration":{"futureConfiguration":{"nested":[1,true,"x"]}},
		"displayMetadata":{"displayName":"Example"},"installationName":"example","marketplaceRid":"ri.marketplace.1",
		"namespaceRid":"ri.namespace.1","roleGrants":[{"role":"owner","principal":"group"}],"targetBlockSetVersionId":"version-1"
	}`)
	var request CreateInstallationForBlockSetRequest
	if err := json.Unmarshal(fixture, &request); err != nil {
		t.Fatal(err)
	}
	if request.ApolloAgentEntityInfo == nil || request.ApolloAgentEntityInfo.ApolloSpaceID != "space" {
		t.Fatalf("agent info = %#v", request.ApolloAgentEntityInfo)
	}
	assertJSONEqual(t, request.Configuration, []byte(`{"futureConfiguration":{"nested":[1,true,"x"]}}`))
	assertJSONEqual(t, request.DisplayMetadata, []byte(`{"displayName":"Example"}`))
	assertJSONEqual(t, request.RoleGrants[0], []byte(`{"role":"owner","principal":"group"}`))

	store := FoundryProductsDownloaderStore{
		ApolloCrossStackManifest: ApolloCrossStackManifest{
			StackConfiguration: ApolloStackManagementConfig(`{"type":"futureVariant","futureVariant":{"enabled":true}}`),
			StoreName:          "store",
		},
		Products: []json.RawMessage{json.RawMessage(`{"mavenCoordinate":"com.example:product:1.0.0"}`)},
	}
	encoded, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	var decoded FoundryProductsDownloaderStore
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, decoded.ApolloCrossStackManifest.StackConfiguration, []byte(`{"type":"futureVariant","futureVariant":{"enabled":true}}`))
	assertJSONEqual(t, decoded.Products[0], []byte(`{"mavenCoordinate":"com.example:product:1.0.0"}`))
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode got %q: %v", got, err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode want %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}
