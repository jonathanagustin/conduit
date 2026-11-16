// Package application projects authorized release observations without mutation.
package application

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/delivery/releasestatus/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/releasestatus/ports"
	"sort"
	"time"
)

type Service struct {
	Source ports.Source
	Now    func() time.Time
}

// Read requires an explicit environment authorizer. Mixed-visibility rollouts
// are omitted completely: their aggregate statuses can reveal hidden members.
func (s Service) Read(ctx context.Context, product, version string, allow func(string) bool) (domain.View, error) {
	if s.Source == nil || allow == nil {
		return domain.View{}, domain.ErrUnavailable
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	snap, err := s.Source.Read(ctx, product, version, now)
	if err != nil {
		return domain.View{}, err
	}
	if !snap.Found {
		return domain.View{}, domain.ErrNotFound
	}
	view := domain.View{RolloutPersistence: "process-local", Product: product, Version: version, ReadAt: now, Environments: []domain.Environment{}, Rollouts: []domain.Rollout{}, ValueTier: domain.ValueTier}
	visibility := map[string]bool{}
	visible := func(id string) bool {
		if id == "" {
			return false
		}
		v, ok := visibility[id]
		if !ok {
			v = allow(id)
			visibility[id] = v
		}
		return v
	}
	environments := map[string]*domain.Environment{}
	ensure := func(id string) *domain.Environment {
		row := environments[id]
		if row == nil {
			row = &domain.Environment{ID: id, Installations: []domain.Installation{}, PromotionGraphs: []domain.Graph{}}
			environments[id] = row
		}
		return row
	}
	rows := 0
	count := func(n int) bool { rows += n; return rows <= domain.MaxRows }
	for _, item := range snap.Installations {
		if item.Product != product || (item.DesiredVersion != version && item.ObservedVersion != version) || !visible(item.Environment) {
			continue
		}
		if !count(1) {
			return domain.View{}, domain.ErrUnavailable
		}
		env := ensure(item.Environment)
		env.Installations = append(env.Installations, item)
	}
	for _, graph := range snap.Graphs {
		if graph.Product != product || !visible(graph.Environment) {
			continue
		}
		if !count(1 + len(graph.Edges)) {
			return domain.View{}, domain.ErrUnavailable
		}
		copy := graph
		copy.Edges = append([]domain.Edge{}, graph.Edges...)
		sort.Slice(copy.Edges, func(i, j int) bool {
			a, b := copy.Edges[i], copy.Edges[j]
			if a.FromChannel != b.FromChannel {
				return a.FromChannel < b.FromChannel
			}
			return a.ToChannel < b.ToChannel
		})
		env := ensure(graph.Environment)
		env.PromotionGraphs = append(env.PromotionGraphs, copy)
	}
	for _, rollout := range snap.Rollouts {
		if rollout.Product != product || rollout.Version != version {
			continue
		}
		complete := len(rollout.Waves) > 0
		for _, wave := range rollout.Waves {
			if len(wave.Environments) == 0 {
				complete = false
			}
			for _, id := range wave.Environments {
				if !visible(id) {
					complete = false
				}
			}
		}
		if !complete {
			continue
		}
		if !count(1 + len(rollout.Waves)) {
			return domain.View{}, domain.ErrUnavailable
		}
		copy := rollout
		copy.Waves = make([]domain.Wave, 0, len(rollout.Waves))
		for _, wave := range rollout.Waves {
			if !count(len(wave.Environments)) {
				return domain.View{}, domain.ErrUnavailable
			}
			w := wave
			w.Environments = append([]string{}, wave.Environments...)
			for _, id := range w.Environments {
				ensure(id)
			}
			copy.Waves = append(copy.Waves, w)
		}
		view.Rollouts = append(view.Rollouts, copy)
	}
	if !count(len(environments)) {
		return domain.View{}, domain.ErrUnavailable
	}
	for _, env := range environments {
		sort.Slice(env.Installations, func(i, j int) bool { return env.Installations[i].InstallationID < env.Installations[j].InstallationID })
		sort.Slice(env.PromotionGraphs, func(i, j int) bool { return env.PromotionGraphs[i].PlanID < env.PromotionGraphs[j].PlanID })
		view.Environments = append(view.Environments, *env)
	}
	sort.Slice(view.Environments, func(i, j int) bool { return view.Environments[i].ID < view.Environments[j].ID })
	sort.Slice(view.Rollouts, func(i, j int) bool { return view.Rollouts[i].ID < view.Rollouts[j].ID })
	return view, nil
}
