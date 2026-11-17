package controlplane

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"
)

// Generated fleets exercise the planner, not a second implementation of it.
// Every Product has one installation in each namespace and dependencies only
// point to earlier Products, so the fixture itself establishes the DAG.
func generatedFleet(seed uint64) DesiredState {
	random := rand.New(rand.NewSource(int64(seed)))
	products, namespaces := 1+random.Intn(8), 1+random.Intn(4)
	state := DesiredState{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
		Environment: Environment{ID: "generated-fleet", Generation: fmt.Sprintf("generation-%016x", seed)},
	}
	for product := 0; product < products; product++ {
		name := fmt.Sprintf("app.capacity:product-%d", product)
		artifact := Artifact{Repository: fmt.Sprintf("registry.example/capacity-%d", product), Tag: "1.0.0", Digest: fmt.Sprintf("sha256:%064x", product+1)}
		release := Release{Product: name, Version: "1.0.0", Chart: artifact, Images: []Artifact{artifact}}
		for earlier := 0; earlier < product; earlier++ {
			if random.Intn(2) == 0 {
				release.Dependencies = append(release.Dependencies, Dependency{Product: fmt.Sprintf("app.capacity:product-%d", earlier), MinimumVersion: "1.0.0", MaximumVersion: "1.0.0"})
			}
		}
		state.Releases = append(state.Releases, release)
		for namespace := 0; namespace < namespaces; namespace++ {
			state.Entities = append(state.Entities, Entity{Name: fmt.Sprintf("product-%d-namespace-%d", product, namespace), Product: name, Version: "1.0.0", Namespace: fmt.Sprintf("namespace-%d", namespace)})
		}
	}
	return state
}

func checkGeneratedFleet(seed uint64) error {
	state := generatedFleet(seed)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	plan, err := BuildPlanWithObserved(state, EnvironmentReport{}, now)
	if err != nil {
		return fmt.Errorf("valid fleet refused: %w", err)
	}
	if len(plan.Steps) != len(state.Entities) {
		return fmt.Errorf("lost or duplicated installations: got %d, want %d", len(plan.Steps), len(state.Entities))
	}
	releases := make(map[string]Release)
	for _, release := range state.Releases {
		releases[release.Product] = release
	}
	seen := make(map[string]PlanStep)
	for index, step := range plan.Steps {
		if step.Position != index+1 || step.InstallationID == "" {
			return fmt.Errorf("unordered or anonymous step: %#v", step)
		}
		if _, duplicate := seen[step.InstallationID]; duplicate {
			return fmt.Errorf("duplicate installation: %s", step.InstallationID)
		}
		release := releases[step.Product]
		if !reflect.DeepEqual(step.Chart, release.Chart) || !reflect.DeepEqual(step.Images, release.Images) || step.Version != release.Version {
			return fmt.Errorf("declared artifact changed for %s", step.InstallationID)
		}
		wanted := make(map[string]bool)
		for _, dependency := range release.Dependencies {
			wanted[dependency.Product] = true
		}
		if len(step.DependsOn) != len(wanted) {
			return fmt.Errorf("dependency omitted or duplicated for %s", step.InstallationID)
		}
		for _, id := range step.DependsOn {
			dependency, earlier := seen[id]
			if !earlier || dependency.Namespace != step.Namespace || !wanted[dependency.Product] {
				return fmt.Errorf("dependency not earlier in the same namespace: %s -> %s", step.InstallationID, id)
			}
			delete(wanted, dependency.Product)
		}
		if len(wanted) != 0 {
			return fmt.Errorf("wrong dependency selection for %s", step.InstallationID)
		}
		seen[step.InstallationID] = step
	}
	// Input order and map iteration must not change the reviewed Plan.
	random := rand.New(rand.NewSource(int64(seed ^ 0x849)))
	for range 8 {
		random.Shuffle(len(state.Entities), func(i, j int) { state.Entities[i], state.Entities[j] = state.Entities[j], state.Entities[i] })
		random.Shuffle(len(state.Releases), func(i, j int) { state.Releases[i], state.Releases[j] = state.Releases[j], state.Releases[i] })
		again, err := BuildPlanWithObserved(state, EnvironmentReport{}, now)
		if err != nil || !reflect.DeepEqual(plan, again) {
			return fmt.Errorf("input permutation changed the Plan: %v", err)
		}
	}
	// A changed chart digest must change exactly the affected release binding.
	changed := state.Releases[0].Product
	state.Releases[0].Chart.Digest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	after, err := BuildPlanWithObserved(state, EnvironmentReport{}, now)
	if err != nil {
		return err
	}
	for _, step := range after.Steps {
		if before := seen[step.InstallationID]; (before.ReleaseDigest != step.ReleaseDigest) != (step.Product == changed) {
			return fmt.Errorf("release digest did not isolate the changed Product: %s", step.Product)
		}
	}
	return nil
}

func TestGeneratedFleetPlannerInvariants(t *testing.T) {
	property := func(seed uint64) bool {
		if err := checkGeneratedFleet(seed); err != nil {
			t.Logf("seed=%d: %v", seed, err)
			return false
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 400, Rand: rand.New(rand.NewSource(849))}); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedFleetPlannerRefusesCycles(t *testing.T) {
	property := func(seed uint64) bool {
		state := generatedFleet(seed)
		for index := range state.Releases {
			previous := (index + len(state.Releases) - 1) % len(state.Releases)
			state.Releases[index].Dependencies = []Dependency{{Product: state.Releases[previous].Product, MinimumVersion: "1.0.0", MaximumVersion: "1.0.0"}}
		}
		_, err := BuildPlan(state)
		return err != nil
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 200, Rand: rand.New(rand.NewSource(850))}); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkGeneratedFleetPlanner(b *testing.B) {
	for _, size := range []int{1, 10, 100, 170} {
		state := generatedFleet(1)
		state.Releases = state.Releases[:1]
		state.Entities = make([]Entity, size)
		for index := range state.Entities {
			state.Entities[index] = Entity{Name: fmt.Sprintf("installation-%04d", index), Product: state.Releases[0].Product, Version: "1.0.0", Namespace: fmt.Sprintf("namespace-%04d", index)}
		}
		b.Run(fmt.Sprintf("installations-%d", len(state.Entities)), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := BuildPlan(state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
