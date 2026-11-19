package controlplane

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type artifactRegistryHistory struct {
	plans       []PlanRecord
	err         error
	calls       int
	context     context.Context
	environment string
}

func (history *artifactRegistryHistory) ListPlans(ctx context.Context, environment string) ([]PlanRecord, error) {
	history.calls++
	history.context, history.environment = ctx, environment
	return history.plans, history.err
}

var _ ArtifactRegistryReader = (*artifactRegistryHistory)(nil)

func TestArtifactRegistryProjectionRequiresOnlyPlanHistory(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	record := func(id, environment string, age time.Duration, assignments ...ArtifactRegistryAssignment) PlanRecord {
		return PlanRecord{ID: id, Plan: Plan{Environment: environment}, CreatedAt: now.Add(age), DesiredState: &DesiredState{Environment: Environment{ID: environment, ArtifactRegistries: assignments}}}
	}
	assignment := ArtifactRegistryAssignment{ID: "shared", URL: "registry.example.test", Revision: 4, Mirrors: []string{"mirror.example.test"}, CredentialRef: "opaque/provider/ref", Authentication: "SECRET", ImagePullSecretName: "registry-pull"}
	history := &artifactRegistryHistory{plans: []PlanRecord{
		record("old", "alpha", -time.Hour, ArtifactRegistryAssignment{ID: "obsolete"}),
		record("new", "alpha", 0, assignment, ArtifactRegistryAssignment{ID: "z-last"}),
		record("beta", "beta", 0, assignment),
		{ID: "command", Plan: Plan{Environment: "command-only"}, CreatedAt: now.Add(time.Hour)},
	}}
	got, err := ListArtifactRegistryRecords(t.Context(), history)
	if err != nil {
		t.Fatal(err)
	}
	if history.calls != 1 || history.context != t.Context() || history.environment != "" {
		t.Fatalf("history request: %#v", history)
	}
	want := []ArtifactRegistryRecord{{Environment: "alpha", Assignment: assignment}, {Environment: "beta", Assignment: assignment}, {Environment: "alpha", Assignment: ArtifactRegistryAssignment{ID: "z-last"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection: got %#v want %#v", got, want)
	}
	got[0].Assignment.Mirrors[0] = "changed"
	again, err := ListArtifactRegistryRecords(t.Context(), history)
	if err != nil || again[0].Assignment.Mirrors[0] != "mirror.example.test" || again[1].Assignment.Mirrors[0] != "mirror.example.test" {
		t.Fatalf("projection aliases history or another Environment: %#v, %v", again, err)
	}
}

func TestArtifactRegistryProjectionHistoryFailureAndEmpty(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("history unavailable")
	history := &artifactRegistryHistory{err: sentinel}
	got, err := ListArtifactRegistryRecords(t.Context(), history)
	if !errors.Is(err, sentinel) || got != nil || history.calls != 1 {
		t.Fatalf("failure: %#v %v calls=%d", got, err, history.calls)
	}
	history.err = nil
	got, err = ListArtifactRegistryRecords(t.Context(), history)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty projection: %#v %v", got, err)
	}
}
