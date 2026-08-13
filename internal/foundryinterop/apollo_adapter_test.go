package foundryinterop

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestRecoveredApolloManagedConfigurationNativeRoundTrip(t *testing.T) {
	recovered := ApolloManagedInstallationConfigurationV0{
		CBACMarkings: []string{"ri.marking.1"},
		InputMapping: map[string]ResolvedBlockSetInputShape{
			"input": ResolvedBlockSetInputShape(`{"type":"futureResolvedShape","futureResolvedShape":{"rid":"ri.object.1"}}`),
		},
		PresetInputMapping: map[string]InputShapeResolver{
			"preset": InputShapeResolver(`{"type":"input","input":{"inputId":"input"}}`),
		},
		SecretInputMapping: map[string]SecretResolvedBlockSetInputShape{
			"edge": secretJSON(t, SecretResolvedBlockSetInputShapeEdgePipelineMagritteSource{
				Type: secretTypeEdgePipelineMagritteSource,
				EdgePipelineMagritteSource: SecretResolvedEdgePipelineMagritteSourceInputShape{
					Config: json.RawMessage(`{"source":"edge"}`), SecretName: "secret-edge", TaskConfig: json.RawMessage(`{"task":"ingest"}`),
				},
			}),
			"markings": secretJSON(t, SecretResolvedBlockSetInputShapeMarkings{
				Type: secretTypeMarkings, Markings: SecretResolvedMarkingsInputShape(ApolloSecretAndKeyName{SecretKeyName: "markings", SecretName: "secret-markings"}),
			}),
			"credential": secretJSON(t, SecretResolvedBlockSetInputShapeNamedCredential{
				Type: secretTypeNamedCredential, NamedCredential: SecretResolvedNamedCredentialInputShape{SecretName: "secret-credential"},
			}),
		},
	}
	native, err := ToNativeManaged(recovered)
	if err != nil {
		t.Fatal(err)
	}
	if native.SecretInputMapping["markings"].Markings.SecretKeyName != "markings" ||
		native.SecretInputMapping["credential"].NamedCredential.SecretName != "secret-credential" {
		t.Fatalf("native secret mappings = %#v", native.SecretInputMapping)
	}
	roundTripped, err := FromNativeManaged(native)
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalEqual(t, recovered, roundTripped)
}

func TestRecoveredApolloUniversalConfigurationNativeRoundTrip(t *testing.T) {
	recovered := ApolloUniversalInstallationConfigurationV0{
		EnrollmentAllowlist: []string{"ri.enrollment.allowed"},
		EnrollmentDenylist:  []string{"ri.enrollment.denied"},
		ManagedConfig: ApolloManagedInstallationConfigurationV0{
			CBACMarkings: []string{}, InputMapping: map[string]ResolvedBlockSetInputShape{},
			PresetInputMapping: map[string]InputShapeResolver{}, SecretInputMapping: map[string]SecretResolvedBlockSetInputShape{},
		},
		NamespaceDenylist: []string{"ri.namespace.denied"},
	}
	native, err := ToNativeUniversal(recovered)
	if err != nil {
		t.Fatal(err)
	}
	roundTripped, err := FromNativeUniversal(native)
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalEqual(t, recovered, roundTripped)
}

func TestRecoveredApolloSecretUnionFailsClosed(t *testing.T) {
	_, err := ToNativeManaged(ApolloManagedInstallationConfigurationV0{
		SecretInputMapping: map[string]SecretResolvedBlockSetInputShape{
			"future": SecretResolvedBlockSetInputShape(`{"type":"futureSecret","futureSecret":{"value":"opaque"}}`),
		},
	})
	if err == nil {
		t.Fatal("unsupported secret variant was accepted")
	}
	_, err = FromNativeManaged(controlplane.ManagedInstallationConfiguration{
		SecretInputMapping: map[string]controlplane.SecretResolvedInput{"future": {Type: "futureSecret"}},
	})
	if err == nil {
		t.Fatal("unsupported native secret variant was accepted")
	}
}

func TestRecoveredApolloConfigurationAdapterIsForwardCompatible(t *testing.T) {
	payload := json.RawMessage(`{
		"cbacMarkings":[],"inputMapping":{},"presetInputMapping":{},"secretInputMapping":{},
		"futureField":{"preservedBySource":true}
	}`)
	managed, universal, requirements, err := AdaptEntityConfiguration(ManagedMode, payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if managed == nil || universal != nil || len(requirements) != 0 {
		t.Fatalf("adapted configuration = managed %#v universal %#v requirements %#v", managed, universal, requirements)
	}
	if _, _, _, err := AdaptEntityConfiguration("future-v1", payload, nil); err == nil {
		t.Fatal("unknown recovered configuration mode was accepted")
	}
}

func TestRecoveredApolloConfigurationRunsThroughNativePlanLifecycle(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state := controlplane.DesiredState{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
		Environment: controlplane.Environment{
			ID: "dev-use2", Generation: "0123456789abcdef",
			Namespaces: []controlplane.NamespaceTarget{{
				RID: "ri.namespace.example", Name: "example", EnrollmentRID: "ri.enrollment.allowed", OntologyCountValid: true,
				Organizations: []controlplane.Organization{{DisplayName: "Example", PrimaryHostname: "example.test"}},
			}},
			Secrets: []controlplane.ExternalSecretAssignment{{
				Name: "runtime", Revision: 1, ProviderRef: "vault://example/runtime",
				KubernetesSecretName: "example-runtime", Keys: []string{"username", "password"},
			}},
		},
		Releases: []controlplane.Release{{
			Product: "com.example:service", Version: "1.2.3",
			Chart:  controlplane.Artifact{Repository: "registry.example/service", Tag: "1.2.3", Digest: digest},
			Images: []controlplane.Artifact{{Repository: "registry.example/service", Tag: "1.2.3", Digest: digest}},
		}},
		Entities: []controlplane.Entity{{
			Name: "service", Product: "com.example:service", Version: "1.2.3", Namespace: "example",
		}},
	}
	recovered := ApolloManagedInstallationConfigurationV0{
		CBACMarkings: []string{"ri.marking.public"},
		InputMapping: map[string]ResolvedBlockSetInputShape{
			"endpoint": ResolvedBlockSetInputShape(`{"url":"https://{{ default_hostname }}/api"}`),
		},
		PresetInputMapping: map[string]InputShapeResolver{},
		SecretInputMapping: map[string]SecretResolvedBlockSetInputShape{
			"credential": secretJSON(t, SecretResolvedBlockSetInputShapeNamedCredential{
				Type: secretTypeNamedCredential, NamedCredential: SecretResolvedNamedCredentialInputShape{SecretName: "runtime"},
			}),
		},
	}
	if err := ApplyManagedConfiguration(&state.Entities[0], recovered, map[string][]string{
		"credential": {"username", "password"},
	}); err != nil {
		t.Fatal(err)
	}
	if errs := controlplane.Validate(state); len(errs) != 0 {
		t.Fatalf("translated recovered configuration failed native validation: %#v", errs)
	}
	plan, err := controlplane.BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ConfigDigest == "" {
		t.Fatalf("native plan did not carry translated configuration: %#v", plan)
	}
	if len(plan.Steps[0].SecretAccess) != 1 || plan.Steps[0].SecretAccess[0].ProviderRef != "vault://example/runtime" {
		t.Fatalf("native plan did not resolve Environment-owned secret metadata: %#v", plan.Steps[0].SecretAccess)
	}
}

func secretJSON(t *testing.T, value any) SecretResolvedBlockSetInputShape {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return SecretResolvedBlockSetInputShape(encoded)
}

func assertCanonicalEqual(t *testing.T, got, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(gotJSON, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantJSON, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("round trip mismatch\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

// AdaptEntityConfiguration has two modes and only one was driven through it.
// The universal branch is a whole translation path -- decode, apply, and the
// secret requirements it derives -- and none of it was reached: the round-trip
// test exercises ToNativeUniversal directly, which is a different entry point.
func TestUniversalModeAdaptsThroughTheSameEntryPointAsManaged(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{
		"enrollmentAllowlist":["ri.enrollment.allowed"],
		"namespaceDenylist":["ri.namespace.denied"],
		"managedConfig":{
			"cbacMarkings":[],"inputMapping":{},"presetInputMapping":{},"secretInputMapping":{}
		}
	}`)
	managed, universal, requirements, err := AdaptEntityConfiguration(UniversalMode, payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The two modes are exclusive: a universal payload must not also present as
	// a managed one, or the Entity would carry both and the Plan would have to
	// choose.
	if universal == nil {
		t.Fatal("universal mode produced no universal configuration")
	}
	if managed != nil {
		t.Fatalf("universal mode also produced a managed configuration: %#v", managed)
	}
	if len(requirements) != 0 {
		t.Fatalf("a payload mapping no secrets produced requirements: %#v", requirements)
	}

	// A payload the mode cannot decode is refused rather than partially applied.
	if _, _, _, err := AdaptEntityConfiguration(UniversalMode, json.RawMessage(`{"managedConfig":[]}`), nil); err == nil {
		t.Fatal("a malformed universal payload was accepted")
	}
	// Unknown fields are tolerated in this mode too, the same way managed mode
	// tolerates them: decodeExact does not disallow them, deliberately, so a
	// Product published against a newer Apollo does not become unreadable here.
	if _, _, _, err := AdaptEntityConfiguration(
		UniversalMode, json.RawMessage(`{"managedConfig":{},"futureField":{"preserved":true}}`), nil,
	); err != nil {
		t.Fatalf("a forward-compatible universal payload was refused: %v", err)
	}
	// What decodeExact does enforce is one document: a second one is a
	// truncated or concatenated payload, not a newer schema.
	if _, _, _, err := AdaptEntityConfiguration(
		UniversalMode, json.RawMessage(`{"managedConfig":{}} {"managedConfig":{}}`), nil,
	); err == nil {
		t.Fatal("two concatenated documents were accepted")
	}
}

// ApplyUniversalConfiguration writes into an Entity the caller owns, so a nil
// one is a programming error it has to report rather than dereference.
func TestApplyUniversalConfigurationRequiresAnEntity(t *testing.T) {
	t.Parallel()
	source := ApolloUniversalInstallationConfigurationV0{
		ManagedConfig: ApolloManagedInstallationConfigurationV0{
			CBACMarkings: []string{}, InputMapping: map[string]ResolvedBlockSetInputShape{},
			PresetInputMapping: map[string]InputShapeResolver{},
			SecretInputMapping: map[string]SecretResolvedBlockSetInputShape{},
		},
	}
	if err := ApplyUniversalConfiguration(nil, source, nil); err == nil {
		t.Fatal("a nil Entity was accepted")
	}

	// Applying universal configuration clears any managed configuration on the
	// Entity: the two are alternatives, and leaving both would let a Plan act on
	// whichever it happened to read.
	entity := &controlplane.Entity{Configuration: &controlplane.ManagedInstallationConfiguration{}}
	if err := ApplyUniversalConfiguration(entity, source, nil); err != nil {
		t.Fatal(err)
	}
	if entity.Configuration != nil {
		t.Fatalf("managed configuration survived a universal apply: %#v", entity.Configuration)
	}
	if entity.Universal == nil {
		t.Fatal("universal configuration was not applied")
	}
}
