package catalogadapter

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
)

func manifest(name string, requires ...controlplane.ApolloManifestDependency) controlplane.ApolloProductManifest {
	return controlplane.ApolloProductManifest{
		ProductName: name,
		Extensions:  controlplane.ApolloProductManifestExtensions{ProductDependencies: requires},
	}
}

func needs(name string) controlplane.ApolloManifestDependency {
	return controlplane.ApolloManifestDependency{ProductName: name}
}

func TestADependencyBecomesStepOrdering(t *testing.T) {
	got, err := Dependencies(
		[]Installation{
			{StepID: "a-hub", Product: "hub", Lane: "spoke-a"},
			{StepID: "a-agent", Product: "agent", Lane: "spoke-a"},
		},
		map[string]controlplane.ApolloProductManifest{"agent": manifest("agent", needs("hub"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Dependency{{Before: "a-hub", After: "a-agent"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// The rule that matters: a dependency is satisfied within a lane. A hub in one
// environment must not unblock an agent in another.
func TestADependencyIsNotSatisfiedFromAnotherLane(t *testing.T) {
	_, err := Dependencies(
		[]Installation{
			{StepID: "a-hub", Product: "hub", Lane: "spoke-a"},
			{StepID: "b-agent", Product: "agent", Lane: "spoke-b"},
		},
		map[string]controlplane.ApolloProductManifest{"agent": manifest("agent", needs("hub"))},
	)
	if err == nil {
		t.Fatal("a cross-lane dependency was treated as satisfied")
	}
	if !strings.Contains(err.Error(), "spoke-b") {
		t.Fatalf("the refusal does not name the lane: %v", err)
	}
}

func TestAnOptionalDependencyThatIsAbsentIsSkipped(t *testing.T) {
	optional := controlplane.ApolloManifestDependency{ProductName: "telemetry", Optional: true}

	got, err := Dependencies(
		[]Installation{{StepID: "a-agent", Product: "agent", Lane: "spoke-a"}},
		map[string]controlplane.ApolloProductManifest{"agent": manifest("agent", optional)},
	)
	if err != nil {
		t.Fatalf("an absent optional dependency was refused: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an absent optional dependency produced ordering: %+v", got)
	}
}

// A product Catalog has never published contributes no ordering. An entity
// carrying it is a different fault, caught where entities are read.
func TestAnUnpublishedProductContributesNoOrdering(t *testing.T) {
	got, err := Dependencies(
		[]Installation{{StepID: "a-thing", Product: "unpublished", Lane: "spoke-a"}},
		map[string]controlplane.ApolloProductManifest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got ordering from nothing: %+v", got)
	}
}

func TestTwoEntitiesOfOneProductBothBecomePredecessors(t *testing.T) {
	got, err := Dependencies(
		[]Installation{
			{StepID: "a-hub-1", Product: "hub", Lane: "spoke-a"},
			{StepID: "a-hub-2", Product: "hub", Lane: "spoke-a"},
			{StepID: "a-agent", Product: "agent", Lane: "spoke-a"},
		},
		map[string]controlplane.ApolloProductManifest{"agent": manifest("agent", needs("hub"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both hub steps to precede the agent, got %+v", got)
	}
}

// A product depending on itself must not order a step against its sibling.
func TestSelfDependencyDoesNotOrderAStepAgainstItself(t *testing.T) {
	got, err := Dependencies(
		[]Installation{{StepID: "a-hub", Product: "hub", Lane: "spoke-a"}},
		map[string]controlplane.ApolloProductManifest{"hub": manifest("hub", needs("hub"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range got {
		if dependency.Before == dependency.After {
			t.Fatalf("a step was ordered against itself: %+v", dependency)
		}
	}
}

// The same fleet must compile to the same problem however it was enumerated,
// or two callers holding the same set produce two digests and nothing
// downstream can be pinned.
//
// Repeating one input proves nothing here: iteration is over a slice, so it is
// already deterministic within a run, and a version of this test that only
// re-ran the same order passed with the sort deleted. Permuting the input is
// what the canonical order is actually for.
func TestOutputIsCanonicalWhateverOrderTheInputArrives(t *testing.T) {
	manifests := map[string]controlplane.ApolloProductManifest{"agent": manifest("agent", needs("hub"))}
	permutations := [][]Installation{
		{
			{StepID: "a-z", Product: "hub", Lane: "spoke-a"},
			{StepID: "a-a", Product: "hub", Lane: "spoke-a"},
			{StepID: "a-agent", Product: "agent", Lane: "spoke-a"},
		},
		{
			{StepID: "a-agent", Product: "agent", Lane: "spoke-a"},
			{StepID: "a-a", Product: "hub", Lane: "spoke-a"},
			{StepID: "a-z", Product: "hub", Lane: "spoke-a"},
		},
		{
			{StepID: "a-a", Product: "hub", Lane: "spoke-a"},
			{StepID: "a-agent", Product: "agent", Lane: "spoke-a"},
			{StepID: "a-z", Product: "hub", Lane: "spoke-a"},
		},
	}

	first, err := Dependencies(permutations[0], manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("expected both hub steps to precede the agent, got %+v", first)
	}

	for index, installations := range permutations[1:] {
		again, err := Dependencies(installations, manifests)
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(first) {
			t.Fatalf("permutation %d changed the count: %+v vs %+v", index+1, again, first)
		}
		for position := range first {
			if again[position] != first[position] {
				t.Fatalf("permutation %d changed the ordering:\n  %+v\n  %+v", index+1, first, again)
			}
		}
	}
}

func TestAnIncompleteInstallationIsRefused(t *testing.T) {
	for name, installation := range map[string]Installation{
		"no step id": {Product: "hub", Lane: "spoke-a"},
		"no product": {StepID: "a-hub", Lane: "spoke-a"},
		"no lane":    {StepID: "a-hub", Product: "hub"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Dependencies([]Installation{installation}, nil); err == nil {
				t.Fatal("an incomplete installation was accepted")
			}
		})
	}
}
