// Package application owns native Environment projection reads through snapshot ports.
package application

import (
	"context"
	"errors"
	"sort"

	"github.com/jonathanagustin/conduit/internal/environments/domain"
)

// ProjectionSource returns per-Environment native snapshots, never repository or
// protocol objects. Keys retain the source Environment grouping/ordering identity.
// Each call must read current durable state; missing data is not a cached answer.
type ProjectionSource interface {
	InventoryByEnvironment(context.Context) (map[string]domain.CompatibilityProjection, error)
	LifecycleByEnvironment(context.Context) (map[string]domain.ReleaseLifecycle, error)
}

// ProjectionReader deterministically aggregates snapshots without write authority.
type ProjectionReader struct{ Source ProjectionSource }

var ErrProjectionSourceUnavailable = errors.New("environment projection source unavailable")

func (reader ProjectionReader) EnvironmentInventory(ctx context.Context) ([]domain.CompatibilityProjection, error) {
	if reader.Source == nil {
		return nil, ErrProjectionSourceUnavailable
	}
	snapshots, err := reader.Source.InventoryByEnvironment(ctx)
	if err != nil {
		return nil, err
	}
	inventory := make([]domain.CompatibilityProjection, 0, len(snapshots))
	for _, environment := range projectionKeys(snapshots) {
		item := snapshots[environment]
		item.Entities = append([]domain.ProjectedEntity(nil), item.Entities...)
		item.Namespaces = append([]domain.ProjectedNamespace(nil), item.Namespaces...)
		inventory = append(inventory, item)
	}
	return inventory, nil
}

func (reader ProjectionReader) ReleaseLifecycle(ctx context.Context) (domain.ReleaseLifecycle, error) {
	if reader.Source == nil {
		return domain.ReleaseLifecycle{}, ErrProjectionSourceUnavailable
	}
	snapshots, err := reader.Source.LifecycleByEnvironment(ctx)
	if err != nil {
		return domain.ReleaseLifecycle{}, err
	}
	var lifecycle domain.ReleaseLifecycle
	for _, environment := range projectionKeys(snapshots) {
		snapshot := snapshots[environment]
		for _, recall := range snapshot.Recalls {
			recall.ExcludedVersions = append([]string(nil), recall.ExcludedVersions...)
			lifecycle.Recalls = append(lifecycle.Recalls, recall)
		}
		lifecycle.Promotions = append(lifecycle.Promotions, snapshot.Promotions...)
	}
	return lifecycle, nil
}

func projectionKeys[T any](snapshots map[string]T) []string {
	keys := make([]string, 0, len(snapshots))
	for key := range snapshots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
