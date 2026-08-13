package optimization

import (
	"strings"
	"testing"
)

func TestPlatformStateAcquisitionRegistryIsCompleteAndExternalGapsExplicit(t *testing.T) {
	requirements, calls := PlatformStateAcquisitionRegistry()
	plan, err := OptimizeAcquisitionCalls(requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Complete || !plan.Optimal || len(plan.Calls) != 3 || len(plan.Unknown) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
	gaps := PlatformExternalStateGaps()
	if len(gaps) != 4 {
		t.Fatalf("external gaps = %#v", gaps)
	}
	for _, gap := range gaps {
		if gap.ID == "" || gap.RequiredProvenance == "" || gap.Reason == "" {
			t.Fatalf("incomplete gap = %#v", gap)
		}
	}
}

func TestExternalStateAcquisitionRegistryIsMechanicallyCompleteButProviderOwned(t *testing.T) {
	requirements, calls := ExternalStateAcquisitionRegistry()
	plan, err := OptimizeAcquisitionCalls(requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Complete || !plan.Optimal || len(plan.Calls) != 4 || len(plan.Unknown) != 0 {
		t.Fatalf("external candidate plan = %#v", plan)
	}
	resolvedRequirements, resolvedCalls, err := ResolveExternalStateAcquisition("dev-use2", "plan-7")
	if err != nil || len(resolvedRequirements) != 4 || len(resolvedCalls) != 4 {
		t.Fatalf("resolved external registry = %#v %#v %v", resolvedRequirements, resolvedCalls, err)
	}
	for _, call := range resolvedCalls {
		if call.Method != "GET" || strings.Contains(call.Endpoint, "{") || call.Source == "platform-hub" {
			t.Fatalf("external candidate call crossed ownership/read boundary = %#v", call)
		}
	}
	if _, _, err := ResolveExternalStateAcquisition("../dev", "plan-7"); err == nil {
		t.Fatal("accepted traversal in external acquisition coordinates")
	}
}
