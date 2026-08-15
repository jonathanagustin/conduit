package domain

import "testing"

func healthyContainer(id string) EntityContainerHealth {
	return EntityContainerHealth{ContainerID: id, Liveness: true, Readiness: true}
}

func TestNormalizeEntityHealthResolvesBottomUpAndOrdersNodes(t *testing.T) {
	t.Parallel()
	details := EntityHealthDetails{Nodes: []EntityNodeHealth{
		{NodeID: "node-b", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{
			{ContainerID: "api", Liveness: true, Readiness: true, Checks: []HealthCheck{
				{ID: "READY", Result: HealthHealthy}, {ID: "MIGRATIONS", Result: HealthWarning},
			}},
		}},
		{NodeID: "node-a", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{healthyContainer("api")}},
	}}
	if issue := NormalizeEntityHealth("entity", &details); issue != nil {
		t.Fatalf("issue = %#v", issue)
	}
	// Nodes are ordered so two reports of the same fleet compare equal.
	if details.Nodes[0].NodeID != "node-a" || details.Nodes[1].NodeID != "node-b" {
		t.Fatalf("nodes were not ordered: %#v", details.Nodes)
	}
	// A container is as bad as its worst check, and a node as bad as its worst
	// container.
	if details.Nodes[1].Containers[0].Result != HealthWarning || details.Nodes[1].Result != HealthWarning {
		t.Fatalf("worst-of did not propagate upward: %#v", details.Nodes[1])
	}
	if details.Nodes[0].Result != HealthHealthy {
		t.Fatalf("a wholly healthy node = %s", details.Nodes[0].Result)
	}
	// Absent check params become an empty map rather than staying nil, so a
	// serialized report has one shape.
	if details.Nodes[1].Containers[0].Checks[0].Params == nil {
		t.Fatal("check params stayed nil")
	}

	// Liveness or readiness failing is an error regardless of the checks.
	unready := EntityHealthDetails{Nodes: []EntityNodeHealth{{
		NodeID: "node-a", ProductVersion: "1.0.0",
		Containers: []EntityContainerHealth{{ContainerID: "api", Liveness: true}},
	}}}
	if issue := NormalizeEntityHealth("entity", &unready); issue != nil {
		t.Fatal(issue)
	}
	if unready.Nodes[0].Result != HealthError {
		t.Fatalf("an unready container = %s", unready.Nodes[0].Result)
	}

	// A node with no containers is UNKNOWN, not healthy. Nothing was observed,
	// and calling that healthy is how an unreported Entity vouches for a Release.
	silent := EntityHealthDetails{Nodes: []EntityNodeHealth{{NodeID: "node-a", ProductVersion: "1.0.0"}}}
	if issue := NormalizeEntityHealth("entity", &silent); issue != nil {
		t.Fatal(issue)
	}
	if silent.Nodes[0].Result != HealthUnknown {
		t.Fatalf("a node reporting no containers = %s", silent.Nodes[0].Result)
	}
}

func TestNormalizeEntityHealthRefusesAReportItCannotTrust(t *testing.T) {
	t.Parallel()
	for name, details := range map[string]EntityHealthDetails{
		"a node without an identifier": {Nodes: []EntityNodeHealth{{ProductVersion: "1.0.0"}}},
		"a node without a version":     {Nodes: []EntityNodeHealth{{NodeID: "node-a"}}},
		"the same node twice": {Nodes: []EntityNodeHealth{
			{NodeID: "node-a", ProductVersion: "1.0.0"}, {NodeID: "node-a", ProductVersion: "1.0.0"},
		}},
		"a container without an identifier": {Nodes: []EntityNodeHealth{{
			NodeID: "node-a", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{{Liveness: true}},
		}}},
		"the same container twice": {Nodes: []EntityNodeHealth{{
			NodeID: "node-a", ProductVersion: "1.0.0",
			Containers: []EntityContainerHealth{healthyContainer("api"), healthyContainer("api")},
		}}},
		"a check identifier outside the contract": {Nodes: []EntityNodeHealth{{
			NodeID: "node-a", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{{
				ContainerID: "api", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "ready", Result: HealthHealthy}},
			}},
		}}},
		"a check result outside the contract": {Nodes: []EntityNodeHealth{{
			NodeID: "node-a", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{{
				ContainerID: "api", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "READY", Result: "FINE"}},
			}},
		}}},
		"the same check twice": {Nodes: []EntityNodeHealth{{
			NodeID: "node-a", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{{
				ContainerID: "api", Liveness: true, Readiness: true,
				Checks: []HealthCheck{
					{ID: "READY", Result: HealthHealthy}, {ID: "READY", Result: HealthHealthy},
				},
			}},
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report := details
			issue := NormalizeEntityHealth("entity", &report)
			if issue == nil {
				t.Fatal("an untrustworthy report was normalized")
			}
			if issue.Path == "" || issue.Message == "" {
				t.Fatalf("issue does not say where or what: %#v", issue)
			}
		})
	}
}

func TestEntityHealthResultAppliesTheDeclaredQuorum(t *testing.T) {
	t.Parallel()
	nodes := func(healthy, unhealthy int) []EntityNodeHealth {
		result := make([]EntityNodeHealth, 0, healthy+unhealthy)
		for index := 0; index < healthy; index++ {
			result = append(result, EntityNodeHealth{Result: HealthHealthy})
		}
		for index := 0; index < unhealthy; index++ {
			result = append(result, EntityNodeHealth{Result: HealthError})
		}
		return result
	}

	for name, testCase := range map[string]struct {
		details EntityHealthDetails
		want    string
	}{
		// No nodes is UNKNOWN: nothing was observed.
		"nothing reported":          {details: EntityHealthDetails{}, want: HealthUnknown},
		"all nodes healthy":         {details: EntityHealthDetails{Nodes: nodes(3, 0)}, want: HealthHealthy},
		"default quorum is all":     {details: EntityHealthDetails{Nodes: nodes(2, 1)}, want: HealthError},
		"any: one is enough":        {details: EntityHealthDetails{Quorum: "ANY", Nodes: nodes(1, 2)}, want: HealthHealthy},
		"any: none is not":          {details: EntityHealthDetails{Quorum: "ANY", Nodes: nodes(0, 2)}, want: HealthError},
		"all but one tolerates one": {details: EntityHealthDetails{Quorum: "ALL_BUT_ONE", Nodes: nodes(2, 1)}, want: HealthHealthy},
		"all but one, not two":      {details: EntityHealthDetails{Quorum: "ALL_BUT_ONE", Nodes: nodes(1, 2)}, want: HealthError},
		// A lone node under ALL_BUT_ONE must still be healthy: tolerating one of
		// one would call an entirely failed Entity healthy.
		"all but one of one":  {details: EntityHealthDetails{Quorum: "ALL_BUT_ONE", Nodes: nodes(0, 1)}, want: HealthError},
		"simple majority met": {details: EntityHealthDetails{Quorum: "SIMPLE_MAJORITY", Nodes: nodes(2, 1)}, want: HealthHealthy},
		"simple majority not": {details: EntityHealthDetails{Quorum: "SIMPLE_MAJORITY", Nodes: nodes(1, 2)}, want: HealthError},
		// A quorum nobody can evaluate has not been met, and is not the same as
		// the strictest reading.
		"unevaluable quorum": {details: EntityHealthDetails{Quorum: "UNKNOWN", Nodes: nodes(3, 0)}, want: HealthIndeterminate},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := EntityHealthResult(testCase.details); got != testCase.want {
				t.Fatalf("%s = %s, want %s", name, got, testCase.want)
			}
		})
	}
}
