package productconfiguration

import (
	"reflect"
	"testing"
)

func TestEffectiveResourceDefaultsAndPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name             string
		in               *ResourceRequirements
		requests, limits ResourceQuantities
	}{
		{name: "absent", requests: ResourceQuantities{CPU: "0.25", Memory: "1G"}},
		{name: "empty", in: &ResourceRequirements{}, requests: ResourceQuantities{CPU: "0.25", Memory: "1G"}},
		{name: "limits become requests", in: &ResourceRequirements{Limits: &ResourceQuantities{CPU: "2", Memory: "4Gi"}}, requests: ResourceQuantities{CPU: "2", Memory: "4Gi"}, limits: ResourceQuantities{CPU: "2", Memory: "4Gi"}},
		{name: "per field fallback", in: &ResourceRequirements{Requests: &ResourceQuantities{Memory: "3Gi"}, Limits: &ResourceQuantities{CPU: "4"}}, requests: ResourceQuantities{CPU: "4", Memory: "3Gi"}, limits: ResourceQuantities{CPU: "4"}},
		{name: "request precedence", in: &ResourceRequirements{Requests: &ResourceQuantities{CPU: "1", Memory: "2Gi"}, Limits: &ResourceQuantities{CPU: "8", Memory: "16Gi"}}, requests: ResourceQuantities{CPU: "1", Memory: "2Gi"}, limits: ResourceQuantities{CPU: "8", Memory: "16Gi"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveResources(tt.in)
			if got.Requests == nil || got.Limits == nil || *got.Requests != tt.requests || *got.Limits != tt.limits {
				t.Fatalf("effective resources=%#v", got)
			}
		})
	}
}

func TestEffectiveResourceValuesAreDetached(t *testing.T) {
	requests := &ResourceQuantities{Memory: "3Gi"}
	limits := &ResourceQuantities{CPU: "2"}
	input := &ResourceRequirements{Requests: requests, Limits: limits}
	got := EffectiveResources(input)
	if *requests != (ResourceQuantities{Memory: "3Gi"}) || *limits != (ResourceQuantities{CPU: "2"}) || got.Requests == requests || got.Limits == limits {
		t.Fatal("resolution mutated/retained input quantities")
	}
	got.Requests.CPU = "changed"
	got.Limits.Memory = "changed"
	if *requests != (ResourceQuantities{Memory: "3Gi"}) || *limits != (ResourceQuantities{CPU: "2"}) {
		t.Fatal("result mutation changed the declaration")
	}
	shared := &ResourceQuantities{CPU: "1", Memory: "2Gi"}
	got = EffectiveResources(&ResourceRequirements{Requests: shared, Limits: shared})
	if got.Requests == got.Limits || got.Requests == shared || got.Limits == shared {
		t.Fatal("output quantities alias each other or the input")
	}
	got.Requests.CPU = "changed"
	if !reflect.DeepEqual(got.Limits, shared) {
		t.Fatal("request mutation changed limits")
	}
}

func TestEffectiveReplicationPreservesDeclaredValues(t *testing.T) {
	if EffectiveReplication(nil) != 2 || EffectiveReplication(&Replication{}) != 2 {
		t.Fatal("unstated replica default changed")
	}
	// Resolution is not validation: zero is an explicit stop, and invalid values
	// remain available to the separate validator rather than being normalized.
	for _, n := range []int{0, 1, 17, -1} {
		input := n
		if got := EffectiveReplication(&Replication{Desired: &input}); got != n || input != n {
			t.Fatalf("replicas %d resolved to %d", n, got)
		}
	}
}
