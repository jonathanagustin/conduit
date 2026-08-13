package controlplane

import (
	"context"
	"sort"
)

// ArtifactRegistryRecord is a read-only projection of a registry assignment
// from the latest desired state for an Environment. Registry credentials remain
// external; only their opaque reference is projected.
type ArtifactRegistryRecord struct {
	Environment string                     `json:"environment"`
	Assignment  ArtifactRegistryAssignment `json:"assignment"`
}

func ListArtifactRegistryRecords(ctx context.Context, repository Repository) ([]ArtifactRegistryRecord, error) {
	plans, err := repository.ListPlans(ctx, "")
	if err != nil {
		return nil, err
	}
	records := []ArtifactRegistryRecord{}
	for environment, state := range latestDesiredStates(plans) {
		for _, assignment := range state.Environment.ArtifactRegistries {
			records = append(records, ArtifactRegistryRecord{
				Environment: environment,
				Assignment:  cloneJSON(assignment),
			})
		}
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].Assignment.ID != records[right].Assignment.ID {
			return records[left].Assignment.ID < records[right].Assignment.ID
		}
		return records[left].Environment < records[right].Environment
	})
	return records, nil
}
