// Package application reads the dependencies already stored on a release and
// lists fleet installations that are behind, with a reason.
package application

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// Dependency is one product a release needs.
type Dependency struct {
	Product        string `json:"product"`
	MinimumVersion string `json:"minimumVersion"`
	MaximumVersion string `json:"maximumVersion"`
	Optional       bool   `json:"optional,omitempty"`
}

// Upgrade is one installation that is behind.
type Upgrade struct {
	Environment     string `json:"environment"`
	InstallationID  string `json:"installationId"`
	Product         string `json:"product"`
	DesiredVersion  string `json:"desiredVersion"`
	ObservedVersion string `json:"observedVersion"`
	Reason          string `json:"reason"`
}

// Dependencies returns the dependencies stored on the newest desired state
// that contains the release. It does not invent a second dependency model.
func Dependencies(ctx context.Context, plans controlplane.Repository, product, version string) ([]Dependency, error) {
	records, err := plans.ListPlans(ctx, "")
	if err != nil {
		return nil, err
	}
	var found []Dependency
	var seen bool
	for _, record := range records {
		if record.DesiredState == nil {
			continue
		}
		for _, release := range record.DesiredState.Releases {
			if release.Product != product || release.Version != version {
				continue
			}
			seen = true
			found = found[:0]
			for _, dependency := range release.Dependencies {
				found = append(found, Dependency{
					Product: dependency.Product, MinimumVersion: dependency.MinimumVersion,
					MaximumVersion: dependency.MaximumVersion, Optional: dependency.Optional,
				})
			}
		}
	}
	if !seen {
		return []Dependency{}, nil
	}
	if found == nil {
		found = []Dependency{}
	}
	return found, nil
}

// Behind lists installations whose observed version differs from the desired
// version. notes supplies a more specific reason keyed by environment and
// installation, such as a blackout or a disconnected site.
func Behind(ctx context.Context, plans controlplane.Repository, notes map[string]string) ([]Upgrade, error) {
	projection, err := controlplane.BuildFleetProjection(ctx, plans, controlplane.FleetQuery{}, timeNow())
	if err != nil {
		return nil, err
	}
	var rows []Upgrade
	for _, item := range projection.Installations {
		if item.DesiredVersion == "" || item.DesiredVersion == item.ObservedVersion {
			continue
		}
		reason := notes[item.Environment+"\x00"+item.InstallationID]
		if reason == "" {
			if item.ObservedVersion == "" {
				reason = "not yet reported"
			} else {
				reason = "observed version is behind the desired version"
			}
		}
		rows = append(rows, Upgrade{
			Environment: item.Environment, InstallationID: item.InstallationID, Product: item.Product,
			DesiredVersion: item.DesiredVersion, ObservedVersion: item.ObservedVersion, Reason: reason,
		})
	}
	if rows == nil {
		rows = []Upgrade{}
	}
	return rows, nil
}

func timeNow() time.Time { return time.Now().UTC() }
