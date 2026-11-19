// Package catalogadapter derives rollout ordering from Catalog's product
// dependencies.
//
// Apollo states dependencies between products; a rollout orders steps. The
// expansion between them is this package's whole job, and it has one rule worth
// stating: a dependency is satisfied within a lane, not across the fleet.
// Installing the hub in one environment does not unblock an agent in another,
// and treating it as though it did is how a rollout runs work against something
// that is not there yet.
package catalogadapter

import (
	"fmt"
	"sort"

	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
)

// Installation is one entity being installed somewhere: the step's identity,
// the product it carries, and the lane it runs on.
type Installation struct {
	StepID  string
	Product string
	Lane    string
}

// Dependencies expands product dependencies into step ordering.
//
// Manifests are keyed by product name. A product with no manifest contributes
// no ordering rather than an error: Catalog holds manifests for what it
// publishes, and an entity carrying something Catalog has never seen is a
// different problem, caught where entities are read.
func Dependencies(
	installations []Installation,
	manifests map[string]productmanifest.Manifest,
) ([]domain.Dependency, error) {
	// Product to the steps installing it, per lane. The lane is part of the key
	// because that is the scope a dependency is satisfied in.
	providers := map[string]map[string][]string{}
	for _, installation := range installations {
		if installation.StepID == "" || installation.Product == "" || installation.Lane == "" {
			return nil, fmt.Errorf("installation is incomplete: %+v", installation)
		}
		if providers[installation.Lane] == nil {
			providers[installation.Lane] = map[string][]string{}
		}
		lane := providers[installation.Lane]
		lane[installation.Product] = append(lane[installation.Product], installation.StepID)
	}

	dependencies := []domain.Dependency{}
	for _, installation := range installations {
		manifest, published := manifests[installation.Product]
		if !published {
			continue
		}

		for _, required := range manifest.Extensions.ProductDependencies {
			name := required.ProductName
			if name == "" {
				return nil, fmt.Errorf("product %q declares a dependency with no name", installation.Product)
			}

			befores := providers[installation.Lane][name]
			if len(befores) == 0 {
				if required.Optional {
					continue
				}
				return nil, fmt.Errorf(
					"step %q needs %q on lane %q, which nothing in this rollout installs there",
					installation.StepID, name, installation.Lane,
				)
			}

			for _, before := range befores {
				// A product that depends on itself across two entities in one
				// lane would order a step against a sibling for no reason.
				if before == installation.StepID {
					continue
				}
				dependencies = append(dependencies, domain.Dependency{
					Before: before,
					After:  installation.StepID,
				})
			}
		}
	}

	// Stable output. The same fleet must compile to the same problem, or two
	// runs produce two different digests and nothing downstream can be pinned.
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].Before != dependencies[j].Before {
			return dependencies[i].Before < dependencies[j].Before
		}
		return dependencies[i].After < dependencies[j].After
	})
	return dependencies, nil
}
