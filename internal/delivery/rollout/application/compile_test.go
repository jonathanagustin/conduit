package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
)

type lanes struct {
	value []domain.Lane
	err   error
}

func (l lanes) RolloutLanes(context.Context) ([]domain.Lane, error) { return l.value, l.err }

type steps struct {
	value []domain.Step
	err   error
}

func (s steps) RolloutSteps(context.Context) ([]domain.Step, error) { return s.value, s.err }

type dependencies struct {
	value []domain.Dependency
	err   error
}

func (d dependencies) RolloutDependencies(context.Context) ([]domain.Dependency, error) {
	return d.value, d.err
}

type windows struct {
	value []domain.Window
	err   error
}

func (w windows) MaintenanceWindows(context.Context) ([]domain.Window, error) {
	return w.value, w.err
}

func workingCompiler() Compiler {
	return Compiler{
		Lanes: lanes{value: []domain.Lane{{ID: "spoke-a", Capacity: map[string]int64{"slots": 1}}}},
		Steps: steps{value: []domain.Step{{
			ID: "install", DurationSeconds: 60, EligibleLanes: []string{"spoke-a"},
			Demand: map[string]int64{"slots": 1},
		}}},
	}
}

func TestCompileAssemblesAValidatedRollout(t *testing.T) {
	rollout, err := workingCompiler().Compile(context.Background(), "test", 3600)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if rollout.Origin != "test" || rollout.HorizonSeconds != 3600 {
		t.Fatalf("identity lost: %+v", rollout)
	}
	if len(rollout.Steps) != 1 || len(rollout.Lanes) != 1 {
		t.Fatalf("content lost: %+v", rollout)
	}
}

// Ordering and maintenance are genuinely optional: one independent step in a
// fleet with no freeze is a real rollout.
func TestOrderingAndMaintenanceAreOptional(t *testing.T) {
	compiler := workingCompiler()
	compiler.Dependencies = nil
	compiler.Windows = nil

	if _, err := compiler.Compile(context.Background(), "test", 3600); err != nil {
		t.Fatalf("a rollout with no ordering was refused: %v", err)
	}
}

// A missing lane source would validate as "no lanes" and send someone to look
// at the fleet. It is a wiring fault and should say so.
func TestAMissingSourceIsNamedAsWiringNotAsAnEmptyFleet(t *testing.T) {
	for name, compiler := range map[string]Compiler{
		"no lane source": {Steps: workingCompiler().Steps},
		"no step source": {Lanes: workingCompiler().Lanes},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compiler.Compile(context.Background(), "test", 3600)
			if err == nil {
				t.Fatal("expected a wiring error")
			}
			if !strings.Contains(err.Error(), "missing a lane or step source") {
				t.Fatalf("wiring fault reported as something else: %v", err)
			}
		})
	}
}

// Each source names itself when it fails, so a read error points at the context
// that produced it rather than at the compiler.
func TestSourceFailuresNameTheSource(t *testing.T) {
	boom := errors.New("boom")

	for name, testCase := range map[string]struct {
		build  func(Compiler) Compiler
		reason string
	}{
		"lanes":        {func(c Compiler) Compiler { c.Lanes = lanes{err: boom}; return c }, "read rollout lanes"},
		"steps":        {func(c Compiler) Compiler { c.Steps = steps{err: boom}; return c }, "read rollout steps"},
		"dependencies": {func(c Compiler) Compiler { c.Dependencies = dependencies{err: boom}; return c }, "read rollout dependencies"},
		"windows":      {func(c Compiler) Compiler { c.Windows = windows{err: boom}; return c }, "read maintenance windows"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testCase.build(workingCompiler()).Compile(context.Background(), "test", 3600)
			if err == nil {
				t.Fatal("expected the read to fail")
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("failure does not name its source:\n  got:  %v\n  want: %q", err, testCase.reason)
			}
			if !errors.Is(err, boom) {
				t.Fatalf("the underlying cause was dropped: %v", err)
			}
		})
	}
}

// An unschedulable rollout is refused here rather than handed to a solver that
// would report INFEASIBLE without saying which step was impossible.
func TestAnUnschedulableRolloutIsRefusedWithItsReason(t *testing.T) {
	compiler := workingCompiler()
	compiler.Steps = steps{value: []domain.Step{{
		ID: "install", DurationSeconds: 600, LatestFinish: 100,
		EligibleLanes: []string{"spoke-a"}, Demand: map[string]int64{"slots": 1},
	}}}

	_, err := compiler.Compile(context.Background(), "test", 3600)
	if err == nil {
		t.Fatal("an impossible deadline was accepted")
	}
	if !strings.Contains(err.Error(), "cannot be scheduled") || !strings.Contains(err.Error(), "cannot finish by") {
		t.Fatalf("refusal does not explain itself: %v", err)
	}
}
