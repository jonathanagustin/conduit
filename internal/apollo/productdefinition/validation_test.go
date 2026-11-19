package productdefinition

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollo/productconfiguration"
	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

func TestCrossDocumentDiagnosticsRetainOrderAndInputs(t *testing.T) {
	manifest, err := productmanifest.Decode(strings.NewReader(`product-group: com.example
product-name: demo
product-version: 1.0.0
product-type: service.v1
extensions:
  public-proxy-endpoints: [api]
  volumes-v2:
    data:
      volume-type:
        durable-volume: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := productconfiguration.Decode(strings.NewReader(`resources:
  requests: {cpu: "2", memory: 2G}
  limits: {cpu: "1", memory: 1G}
replication: {desired: 0}
conf: {tags: [same, same]}
`))
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Manifest: manifest, Configuration: configuration}
	expected := []validationdomain.Problem{
		{Path: "configuration.resources.requests.cpu", Message: "must not exceed the CPU limit"},
		{Path: "configuration.resources.requests.memory", Message: "must not exceed the memory limit"},
		{Path: "configuration.replication.desired", Message: "must be greater than zero"},
		{Path: "manifest.extensions.volumes-v2.data", Message: "must have corresponding configuration under the volumes key"},
		{Path: "manifest.extensions.public-proxy-endpoints[0]", Message: "must name an endpoints.definitions entry"},
		{Path: "configuration.conf.tags[1]", Message: "must not repeat a tag"},
	}
	for range 20 {
		if actual := ValidateConfiguration(input); !reflect.DeepEqual(actual, expected) {
			t.Fatalf("diagnostics: %#v", actual)
		}
	}
	if *configuration.Replication.Desired != 0 || configuration.Resources.Requests.CPU != "2" || len(manifest.Extensions.VolumesV2) != 1 {
		t.Fatal("validation changed input")
	}
}

func TestVolumeOverrideRetainsCrossUnitComparison(t *testing.T) {
	for _, sizes := range [][2]string{{"5G", "5G"}, {"999G", "1T"}} {
		if err := ValidateVolumeOverride("data", sizes[0], sizes[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateVolumeOverride("data", "1T", "999G"); err == nil || err.Error() != "volume data may only be upsized: 999G is smaller than 1T" {
		t.Fatalf("shrink diagnostic: %v", err)
	}
}
