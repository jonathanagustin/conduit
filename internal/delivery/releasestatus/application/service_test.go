package application

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/delivery/releasestatus/domain"
	"testing"
	"time"
)

type source struct {
	snapshot domain.Snapshot
	err      error
}

func (s source) Read(context.Context, string, string, time.Time) (domain.Snapshot, error) {
	return s.snapshot, s.err
}
func TestReadFiltersEverySurfaceAndPreservesUnknownObservations(t *testing.T) {
	at := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	snap := domain.Snapshot{Found: true, Installations: []domain.Installation{
		{Environment: "visible", InstallationID: "app", Product: "app", DesiredVersion: "1.0.0", Health: "UNKNOWN", RolloutStatus: "UNKNOWN", ConfigStatus: "INDETERMINATE", Lifecycle: "UNKNOWN"},
		{Environment: "secret", InstallationID: "hidden", Product: "app", ObservedVersion: "1.0.0"},
		{Environment: "visible", InstallationID: "other", Product: "other", DesiredVersion: "1.0.0"},
		{Environment: "visible", InstallationID: "unrelated", Product: "app", DesiredVersion: "2.0.0", ObservedVersion: "2.0.0"},
	}, Graphs: []domain.Graph{{Environment: "secret", Product: "app", PlanID: "hidden-plan"}, {Environment: "visible", Product: "app", PlanID: "current", Enabled: true, Edges: []domain.Edge{{FromChannel: "STAGING", ToChannel: "PROD"}, {FromChannel: "DEV", ToChannel: "STAGING"}}}}, Rollouts: []domain.Rollout{
		{ID: "mixed", Product: "app", Version: "1.0.0", Status: "completed", Waves: []domain.Wave{{Name: "private", Environments: []string{"visible", "secret"}, Status: "passed"}}},
		{ID: "visible", Product: "app", Version: "1.0.0", Waves: []domain.Wave{{Name: "second", Environments: []string{"visible"}, Gate: "approval", Status: "pending"}, {Name: "first", Environments: []string{"visible"}, Gate: "health", Status: "passed"}}},
		{ID: "wrong-release", Product: "app", Version: "2.0.0", Waves: []domain.Wave{{Environments: []string{"visible"}}}},
	}}
	service := Service{Source: source{snapshot: snap}, Now: func() time.Time { return at }}
	calls := map[string]int{}
	view, err := service.Read(t.Context(), "app", "1.0.0", func(id string) bool { calls[id]++; return id == "visible" })
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Environments) != 1 || view.Environments[0].ID != "visible" || len(view.Environments[0].Installations) != 1 {
		t.Fatalf("leaked/filter error: %#v", view)
	}
	row := view.Environments[0].Installations[0]
	if row.ObservedVersion != "" || row.Health != "UNKNOWN" || !row.ObservedAt.IsZero() {
		t.Fatalf("invented observation: %#v", row)
	}
	if len(view.Environments[0].PromotionGraphs) != 1 || view.Environments[0].PromotionGraphs[0].Edges[0].FromChannel != "DEV" {
		t.Fatalf("graph: %#v", view)
	}
	if len(view.Rollouts) != 1 || view.Rollouts[0].ID != "visible" || view.Rollouts[0].Waves[0].Name != "second" {
		t.Fatalf("wave order/visibility: %#v", view.Rollouts)
	}
	if !view.ReadAt.Equal(at) || view.ValueTier != domain.ValueTier || view.RolloutPersistence != "process-local" {
		t.Fatalf("provenance: %#v", view)
	}
	if calls["visible"] != 1 || calls["secret"] != 1 {
		t.Fatalf("authority not memoized: %v", calls)
	}
	view.Rollouts[0].Waves[0].Environments[0] = "changed"
	view.Environments[0].PromotionGraphs[0].Edges[0].FromChannel = "changed"
	if snap.Rollouts[1].Waves[0].Environments[0] != "visible" || snap.Graphs[1].Edges[1].FromChannel != "DEV" {
		t.Fatal("mutated source snapshot")
	}
	all, err := service.Read(t.Context(), "app", "1.0.0", func(string) bool { return true })
	if err != nil || len(all.Rollouts) != 2 {
		t.Fatalf("authorized wave missing: %#v %v", all, err)
	}
}
func TestReadFailsClosedAndDoesNotTruncate(t *testing.T) {
	allow := func(string) bool { return true }
	if _, err := (Service{}).Read(t.Context(), "app", "1", allow); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
	service := Service{Source: source{snapshot: domain.Snapshot{Found: true}}}
	if _, err := service.Read(t.Context(), "app", "1", nil); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
	service.Source = source{}
	if _, err := service.Read(t.Context(), "app", "1", allow); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	rows := make([]domain.Installation, domain.MaxRows+1)
	for i := range rows {
		rows[i] = domain.Installation{Environment: "hidden", Product: "app", DesiredVersion: "1"}
	}
	service.Source = source{snapshot: domain.Snapshot{Found: true, Installations: rows}}
	if _, err := service.Read(t.Context(), "app", "1", allow); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("silently truncated: %v", err)
	}
	view, err := service.Read(t.Context(), "app", "1", func(string) bool { return false })
	if err != nil || len(view.Environments) != 0 {
		t.Fatalf("hidden rows leaked through bounds: %#v %v", view, err)
	}
	injected := errors.New("private storage detail")
	service.Source = source{err: injected}
	if _, err := service.Read(t.Context(), "app", "1", allow); !errors.Is(err, injected) {
		t.Fatal(err)
	}
}
