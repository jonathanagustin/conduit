package rubixsubstrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type beanFixture struct {
	Aliases map[string]string            `json:"aliases"`
	Enums   map[string][]string          `json:"enums"`
	Unions  map[string]map[string]string `json:"unions"`
	Objects map[string][]beanField       `json:"objects"`
}

type beanField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func loadBeans(t *testing.T) beanFixture {
	t.Helper()
	path := filepath.Join("..", "..", "deploy", "rubix-substrate", "wire-beans.json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recovered wire-beans fixture: %v", err)
	}
	var parsed beanFixture
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		t.Fatalf("parse recovered wire-beans fixture: %v", err)
	}
	return parsed
}

// objectRegistry maps each recovered object name to its Go model. Every object
// in the fixture must appear here, and every field must line up by JSON tag.
func objectRegistry() map[string]reflect.Type {
	return map[string]reflect.Type{
		"ApplyNetworkEgressConfigurationRequest":           reflect.TypeOf(ApplyNetworkEgressConfigurationRequest{}),
		"ApplyNetworkEgressConfigurationResponse":          reflect.TypeOf(ApplyNetworkEgressConfigurationResponse{}),
		"ApplyNetworkIngressRollbackResponse":              reflect.TypeOf(ApplyNetworkIngressRollbackResponse{}),
		"SetDomainIngressResponse":                         reflect.TypeOf(SetDomainIngressResponse{}),
		"DomainEgressConfiguration":                        reflect.TypeOf(DomainEgressConfiguration{}),
		"TcpEgressConfiguration":                           reflect.TypeOf(TCPEgressConfiguration{}),
		"PortRange":                                        reflect.TypeOf(PortRange{}),
		"IngressConfiguration":                             reflect.TypeOf(IngressConfiguration{}),
		"GetFallbackConfigResponse":                        reflect.TypeOf(GetFallbackConfigResponse{}),
		"SetDomainIngressRequest":                          reflect.TypeOf(SetDomainIngressRequest{}),
		"BindCredentialRequest":                            reflect.TypeOf(BindCredentialRequest{}),
		"Cluster":                                          reflect.TypeOf(Cluster{}),
		"ClusterConfigurations":                            reflect.TypeOf(ClusterConfigurations{}),
		"RetrieveClusterConfigurationsRequest":             reflect.TypeOf(RetrieveClusterConfigurationsRequest{}),
		"RetrieveCredentialForInfraUserPayload":            reflect.TypeOf(RetrieveCredentialForInfraUserPayload{}),
		"RetrieveCredentialForInfraUserRequest":            reflect.TypeOf(RetrieveCredentialForInfraUserRequest{}),
		"RetrieveCredentialPayload":                        reflect.TypeOf(RetrieveCredentialPayload{}),
		"RetrieveCredentialRequest":                        reflect.TypeOf(RetrieveCredentialRequest{}),
		"RetrieveServiceAccountCredentialFromTokenPayload": reflect.TypeOf(RetrieveServiceAccountCredentialFromTokenPayload{}),
		"ValidateConfigRequest":                            reflect.TypeOf(ValidateConfigRequest{}),
		"ValidationPayload":                                reflect.TypeOf(ValidationPayload{}),
	}
}

// unionRegistry maps each recovered union to its Go model.
func unionRegistry() map[string]reflect.Type {
	return map[string]reflect.Type{
		"EgressConfiguration": reflect.TypeOf(EgressConfiguration{}),
		"PortSpecifier":       reflect.TypeOf(PortSpecifier{}),
	}
}

// aliasRegistry / enumRegistry are declared so a recovered alias or enum cannot
// be dropped: the coverage test requires the fixture and registry key sets to be
// identical.
func aliasRegistry() map[string]bool {
	return map[string]bool{
		"Cidr": true, "CountryIsoCode": true, "Domain": true, "Protocol": true, "Port": true,
		"ClusterRoleName": true, "ConcreteNamespace": true, "Credential": true, "EnvironmentId": true,
		"NamespaceType": true, "Password": true, "Scope": true, "ServiceName": true, "Username": true,
		"EgressConfigurations": true, "GetEgressCidrsResponse": true, "IngressConfigurations": true,
	}
}

func enumRegistry() map[string][]string {
	return map[string][]string{
		"ClusterMarkings": {string(ClusterMarkingInternalRoutingAvailable), string(ClusterMarkingEnvironmentProxyClientRequired), string(ClusterMarkingUnknown)},
	}
}

func jsonTags(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// TestObjectFieldsMatchRecovered proves each recovered object is modeled, and its
// Go JSON tags are exactly the recovered Conjure field names.
func TestObjectFieldsMatchRecovered(t *testing.T) {
	fixture := loadBeans(t)
	registry := objectRegistry()

	if !equalSets(keys(fixture.Objects), keys(registry)) {
		t.Fatalf("object coverage drift:\n  fixture: %v\n  models:  %v", keys(fixture.Objects), keys(registry))
	}
	for name, fields := range fixture.Objects {
		want := make([]string, 0, len(fields))
		for _, f := range fields {
			want = append(want, f.Name)
		}
		got := jsonTags(registry[name])
		if !equalSets(want, got) {
			t.Fatalf("%s field drift:\n  recovered: %v\n  model:     %v", name, sortedCopy(want), got)
		}
	}
}

// TestUnionVariantsMatchRecovered proves each union models the discriminant plus
// exactly the recovered variant tags.
func TestUnionVariantsMatchRecovered(t *testing.T) {
	fixture := loadBeans(t)
	registry := unionRegistry()

	if !equalSets(keys(fixture.Unions), keys(registry)) {
		t.Fatalf("union coverage drift:\n  fixture: %v\n  models:  %v", keys(fixture.Unions), keys(registry))
	}
	for name, variants := range fixture.Unions {
		want := []string{"type"}
		for variant := range variants {
			want = append(want, variant)
		}
		got := jsonTags(registry[name])
		if !equalSets(want, got) {
			t.Fatalf("%s union drift:\n  recovered: %v\n  model:     %v", name, sortedCopy(want), got)
		}
	}
}

// TestAliasAndEnumCoverage proves no recovered alias or enum was dropped.
func TestAliasAndEnumCoverage(t *testing.T) {
	fixture := loadBeans(t)

	if !equalSets(keys(fixture.Aliases), keysBool(aliasRegistry())) {
		t.Fatalf("alias coverage drift:\n  fixture: %v\n  models:  %v", keys(fixture.Aliases), keysBool(aliasRegistry()))
	}
	enums := enumRegistry()
	if !equalSets(keys(fixture.Enums), keys(enums)) {
		t.Fatalf("enum coverage drift:\n  fixture: %v\n  models:  %v", keys(fixture.Enums), keys(enums))
	}
	for name, values := range fixture.Enums {
		if !equalSets(values, enums[name]) {
			t.Fatalf("%s enum value drift:\n  recovered: %v\n  model:     %v", name, values, enums[name])
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysBool(m map[string]bool) []string { return keys(m) }

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
