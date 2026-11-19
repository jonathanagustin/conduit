package productmanifest

import (
	"reflect"
	"testing"

	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

func TestServiceExtensionDiagnosticsAreStableAndAppend(t *testing.T) {
	extensions := Extensions{
		VolumesV2:            map[string]Volume{"z_bad": {}, "a-bad": {}},
		PublicProxyEndpoints: []string{"api", "api", " "},
		PublicTCPEndpoints:   []string{"api"}, PublicUDPEndpoints: []string{"api"},
		QuorumRequirement: "unknown", UpgradeStrategy: "unknown",
	}
	expected := []validationdomain.Problem{
		{Path: "prior", Message: "retained"},
		{Path: "extensions.volumes-v2.a-bad.volume-type", Message: "must declare a volume type; durable-volume is the only known type"},
		{Path: "extensions.volumes-v2.z_bad", Message: "volume name must be alphanumerics and hyphens"},
		{Path: "extensions.volumes-v2.z_bad.volume-type", Message: "must declare a volume type; durable-volume is the only known type"},
		{Path: "extensions.public-proxy-endpoints[1]", Message: "must not repeat an endpoint name"},
		{Path: "extensions.public-proxy-endpoints[2]", Message: "must name an endpoints.definitions entry"},
		{Path: "extensions.public-tcp-endpoints[0]", Message: "must not also appear in public-proxy-endpoints"},
		{Path: "extensions.public-udp-endpoints[0]", Message: "must not also appear in public-proxy-endpoints"},
		{Path: "extensions.quorum-requirement", Message: "must be all, all-but-one, any, or simple-majority"},
		{Path: "extensions.upgrade-strategy", Message: "must be rolling or shutdown"},
	}
	for range 20 {
		actual := []validationdomain.Problem{{Path: "prior", Message: "retained"}}
		ValidateServiceExtensions(extensions, &actual)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("diagnostics: %#v", actual)
		}
	}
}

func TestServiceExtensionsPermitSharedRawProtocolsAndDocumentedPolicies(t *testing.T) {
	for _, quorum := range []string{"", QuorumAll, QuorumAllButOne, QuorumAny, QuorumSimpleMajority} {
		for _, strategy := range []string{"", UpgradeStrategyRolling, UpgradeStrategyShutdown} {
			var problems []validationdomain.Problem
			ValidateServiceExtensions(Extensions{
				VolumesV2:          map[string]Volume{"data": {VolumeType: VolumeType{DurableVolume: &DurableVolume{}}}},
				PublicTCPEndpoints: []string{"shared"}, PublicUDPEndpoints: []string{"shared"},
				QuorumRequirement: quorum, UpgradeStrategy: strategy,
			}, &problems)
			if len(problems) != 0 {
				t.Fatalf("%q/%q: %v", quorum, strategy, problems)
			}
		}
	}
}
